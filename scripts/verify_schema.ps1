[CmdletBinding()]
param(
    [string]$MySQLDsn = $env:AI_SHOPPING_MYSQL_DSN,
    [ValidateRange(1, 300)]
    [int]$TimeoutSeconds = 60,
    [string]$ComposeFile = 'deploy/docker-compose.yml'
)

$ErrorActionPreference = 'Stop'
$expectedSchemas = @('agent_db', 'catalog_db', 'knowledge_db', 'trade_db', 'user_db')

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
        Password = $match.Groups['password'].Value
        Host = $match.Groups['host'].Value
        Port = $match.Groups['port'].Value
        Database = $match.Groups['database'].Value
    }
}

if ([string]::IsNullOrWhiteSpace($MySQLDsn)) {
    Write-Error 'Set AI_SHOPPING_MYSQL_DSN or pass -MySQLDsn. The value is intentionally not echoed.'
    exit 1
}

try {
    $connection = ConvertFrom-MySQLDsn -Dsn $MySQLDsn
}
catch {
    Write-Error $_.Exception.Message
    exit 1
}

if (-not (Test-Path -LiteralPath $ComposeFile)) {
    Write-Error "Compose file '$ComposeFile' was not found; local MySQL cannot be verified."
    exit 1
}

try {
    $mysqlContainerOutput = @(docker compose -f $ComposeFile ps -q mysql 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw 'Docker Compose could not query the MySQL service. Ensure Docker is running and required Compose variables are set.'
    }
    $mysqlContainer = ($mysqlContainerOutput | Out-String).Trim()
    if ([string]::IsNullOrWhiteSpace($mysqlContainer)) {
        throw 'MySQL Compose service is not running.'
    }

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $healthOutput = @(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' $mysqlContainer 2>&1)
        if ($LASTEXITCODE -ne 0) {
            throw 'Docker could not inspect the MySQL container while waiting for health.'
        }
        $health = ($healthOutput | Out-String).Trim()
        if ($health -eq 'healthy') {
            break
        }
        if ((Get-Date) -ge $deadline) {
            throw "MySQL did not become healthy within $TimeoutSeconds seconds (last health: $health)."
        }
        Start-Sleep -Seconds 2
    } while ($true)

    $query = "SELECT schema_name FROM information_schema.schemata WHERE schema_name REGEXP '_db$' ORDER BY schema_name;"
    $queryHost = if ($connection.Host -in @('127.0.0.1', 'localhost', '::1')) { 'mysql' } else { $connection.Host }
    $actualSchemas = @(docker compose -f $ComposeFile exec -T -e "MYSQL_PWD=$($connection.Password)" mysql mysql --protocol=TCP --host=$queryHost --port=$($connection.Port) --user=$($connection.User) --database=$($connection.Database) --batch --skip-column-names -e $query 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw 'MySQL schema query failed.'
    }

    $actualSchemas = @($actualSchemas | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | ForEach-Object { $_.Trim() } | Sort-Object -Unique)
    $difference = Compare-Object -ReferenceObject $expectedSchemas -DifferenceObject $actualSchemas
    if ($difference) {
        Write-Error ('Unexpected application schema set. Expected: {0}; actual: {1}.' -f ($expectedSchemas -join ', '), ($actualSchemas -join ', '))
        exit 1
    }

    Write-Output ('Verified application schemas: {0}' -f ($actualSchemas -join ', '))
}
catch {
    $message = if ($null -ne $_ -and $null -ne $_.Exception) {
        $_.Exception.Message
    }
    else {
        'Schema verification failed before MySQL could be queried.'
    }
    Write-Error $message
    exit 1
}
