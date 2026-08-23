[CmdletBinding()]
param(
    [string]$MigrationDirectory = 'deploy/mysql/migrations/catalog',
    [string]$ComposeFile = 'deploy/docker-compose.yml'
)

$ErrorActionPreference = 'Stop'
$dsn = $env:AI_SHOPPING_MYSQL_DSN

function ConvertFrom-MySQLDsn {
    param([Parameter(Mandatory)][string]$Dsn)

    $match = [regex]::Match(
        $Dsn,
        '^(?<user>[^:@/]+):(?<password>.+)@tcp\((?<host>[^:()]+):(?<port>[1-9][0-9]{0,4})\)/(?<database>[^?\s/]+)(?:\?.*)?$'
    )
    if (-not $match.Success) {
        throw 'MySQL DSN must use Go MySQL syntax user:password@tcp(host:port)/database; its value is not echoed.'
    }
    if ($match.Groups['database'].Value -ne 'catalog_db') {
        throw 'AI_SHOPPING_MYSQL_DSN must target catalog_db for catalog migrations; its value is intentionally not echoed.'
    }

    [pscustomobject]@{ User = $match.Groups['user'].Value; Host = $match.Groups['host'].Value; Port = $match.Groups['port'].Value }
}

if ([string]::IsNullOrWhiteSpace($dsn)) { throw 'Set AI_SHOPPING_MYSQL_DSN. The value is intentionally not echoed.' }
if (-not (Test-Path -LiteralPath $MigrationDirectory -PathType Container)) { throw "Migration directory '$MigrationDirectory' was not found." }
if (-not (Test-Path -LiteralPath $ComposeFile -PathType Leaf)) { throw "Compose file '$ComposeFile' was not found." }

$connection = ConvertFrom-MySQLDsn -Dsn $dsn
$queryHost = if ($connection.Host -in @('127.0.0.1', 'localhost', '::1')) { 'mysql' } else { $connection.Host }
$queryPort = if ($queryHost -eq 'mysql') { '3306' } else { $connection.Port }

function Invoke-CatalogMySQL {
    param([Parameter(Mandatory)][string]$Sql)
    $output = @($Sql | docker compose -f $ComposeFile exec -T mysql sh -c 'exec mysql --protocol=TCP --host="$1" --port="$2" --user="$3" --password="$MYSQL_PASSWORD" --database=catalog_db --batch --skip-column-names' sh $queryHost $queryPort $connection.User 2>&1 | ForEach-Object { $_.ToString() })
    if ($LASTEXITCODE -ne 0) { throw 'Catalog schema migration query failed.' }
    return @($output | Where-Object { $_ -notmatch '^mysql: \[Warning\] Using a password on the command line interface can be insecure\.$' })
}

Invoke-CatalogMySQL -Sql 'CREATE TABLE IF NOT EXISTS schema_migrations (version VARCHAR(128) NOT NULL, applied_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), PRIMARY KEY (version)) ENGINE=InnoDB;' | Out-Null
$applied = @(Invoke-CatalogMySQL -Sql 'SELECT version FROM schema_migrations ORDER BY version;' | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | ForEach-Object { $_.Trim() })
foreach ($migration in @(Get-ChildItem -LiteralPath $MigrationDirectory -File -Filter '*.sql' | Sort-Object Name)) {
    $version = $migration.BaseName
    if ($applied -contains $version) { Write-Output "Migration $version already applied."; continue }
    Invoke-CatalogMySQL -Sql (Get-Content -LiteralPath $migration.FullName -Raw) | Out-Null
    $escapedVersion = $version.Replace("'", "''")
    Invoke-CatalogMySQL -Sql "INSERT INTO schema_migrations (version) VALUES ('$escapedVersion');" | Out-Null
    Write-Output "Applied migration $version."
}
