[CmdletBinding()]
param(
    [string]$MigrationDirectory = 'deploy/mysql/migrations',
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

    [pscustomobject]@{
        User = $match.Groups['user'].Value
        Host = $match.Groups['host'].Value
        Port = $match.Groups['port'].Value
    }
}

if ([string]::IsNullOrWhiteSpace($dsn)) {
    throw 'Set AI_SHOPPING_MYSQL_DSN. The value is intentionally not echoed.'
}
if (-not (Test-Path -LiteralPath $MigrationDirectory -PathType Container)) {
    throw "Migration directory '$MigrationDirectory' was not found."
}
if (-not (Test-Path -LiteralPath $ComposeFile -PathType Leaf)) {
    throw "Compose file '$ComposeFile' was not found."
}

$connection = ConvertFrom-MySQLDsn -Dsn $dsn
$queryHost = if ($connection.Host -in @('127.0.0.1', 'localhost', '::1')) { 'mysql' } else { $connection.Host }
$queryPort = if ($queryHost -eq 'mysql') { '3306' } else { $connection.Port }

function Invoke-TradeMySQL {
    param([Parameter(Mandatory)][string]$Sql)

    $output = @($Sql | docker compose -f $ComposeFile exec -T mysql sh -c 'exec mysql --protocol=TCP --host="$1" --port="$2" --user="$3" --password="$MYSQL_PASSWORD" --database=trade_db --batch --skip-column-names' sh $queryHost $queryPort $connection.User 2>&1 | ForEach-Object { $_.ToString() })
    if ($LASTEXITCODE -ne 0) {
        throw 'Trade schema migration query failed.'
    }
    return @($output | Where-Object { $_ -notmatch '^mysql: \[Warning\] Using a password on the command line interface can be insecure\.$' })
}

Invoke-TradeMySQL -Sql 'CREATE TABLE IF NOT EXISTS schema_migrations (version VARCHAR(128) NOT NULL, applied_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), PRIMARY KEY (version)) ENGINE=InnoDB;' | Out-Null
$applied = @(Invoke-TradeMySQL -Sql 'SELECT version FROM schema_migrations ORDER BY version;' | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | ForEach-Object { $_.Trim() })

foreach ($migration in @(Get-ChildItem -LiteralPath $MigrationDirectory -File -Filter '*.sql' | Sort-Object Name)) {
    $version = $migration.BaseName
    if ($applied -contains $version) {
        Write-Output "Migration $version already applied."
        continue
    }

    $sql = Get-Content -LiteralPath $migration.FullName -Raw
    Invoke-TradeMySQL -Sql $sql | Out-Null
    $escapedVersion = $version.Replace("'", "''")
    Invoke-TradeMySQL -Sql "INSERT INTO schema_migrations (version) VALUES ('$escapedVersion');" | Out-Null
    Write-Output "Applied migration $version."
}
