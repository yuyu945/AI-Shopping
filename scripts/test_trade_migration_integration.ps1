[CmdletBinding()]
param(
    [ValidateRange(1025, 65535)]
    [int]$MySQLPort = 33306,
    [switch]$KeepEnvironment
)

$ErrorActionPreference = 'Stop'
if ($env:AI_SHOPPING_TRADE_MIGRATION_INTEGRATION -ne '1') {
    Write-Output 'SKIP: set AI_SHOPPING_TRADE_MIGRATION_INTEGRATION=1 to run the isolated trade migration integration test.'
    exit 0
}
if ([string]::IsNullOrWhiteSpace($env:MYSQL_PASSWORD) -or [string]::IsNullOrWhiteSpace($env:MYSQL_ROOT_PASSWORD)) {
    throw 'MYSQL_PASSWORD and MYSQL_ROOT_PASSWORD must be set for the isolated integration environment.'
}

$runID = [guid]::NewGuid().ToString('N')
$project = "m21trade$runID"
$previousProject = $env:COMPOSE_PROJECT_NAME
$previousPort = $env:MYSQL_PORT
$env:COMPOSE_PROJECT_NAME = $project
$env:MYSQL_PORT = $MySQLPort.ToString()
$composeFile = 'deploy/docker-compose.yml'
$container = "$project-mysql-1"

function Invoke-RootSQL {
    param([Parameter(Mandatory)][string]$Sql)
    $output = @($Sql | docker compose -f $composeFile exec -T -e "MYSQL_PWD=$($env:MYSQL_ROOT_PASSWORD)" mysql mysql -uroot --database=trade_db --batch --skip-column-names 2>&1)
    if ($LASTEXITCODE -ne 0) { throw 'Isolated MySQL assertion query failed.' }
    return $output
}

try {
    docker compose -f $composeFile up -d mysql | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not start isolated MySQL Compose service.' }
    $deadline = (Get-Date).AddSeconds(90)
    do {
        $health = (docker inspect --format '{{.State.Health.Status}}' $container 2>$null).Trim()
        if ($health -eq 'healthy') { break }
        if ((Get-Date) -ge $deadline) { throw 'Isolated MySQL did not become healthy within 90 seconds.' }
        Start-Sleep -Seconds 2
    } while ($true)

    Invoke-RootSQL -Sql 'DROP TABLE IF EXISTS order_items; DROP TABLE IF EXISTS cart_items; DROP TABLE IF EXISTS orders; DROP TABLE IF EXISTS carts; DROP TABLE IF EXISTS schema_migrations;' | Out-Null
    $dsn = "app:$($env:MYSQL_PASSWORD)@tcp(127.0.0.1:$MySQLPort)/trade_db"
    & "$PSScriptRoot\apply_migrations.ps1" -MySQLDsn $dsn -ComposeFile $composeFile
    if ($LASTEXITCODE -ne 0) { throw 'First migration execution failed.' }
    & "$PSScriptRoot\apply_migrations.ps1" -MySQLDsn $dsn -ComposeFile $composeFile
    if ($LASTEXITCODE -ne 0) { throw 'Second migration execution failed.' }

    $result = @(Invoke-RootSQL -Sql "SELECT COUNT(*) FROM schema_migrations WHERE version = '20260822_m2_1_trade_schema'; SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'trade_db' AND table_name IN ('carts','cart_items','orders','order_items'); SELECT COUNT(*) FROM information_schema.table_constraints WHERE table_schema = 'trade_db' AND constraint_type = 'FOREIGN KEY'; SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = 'trade_db' AND numeric_precision = 12 AND numeric_scale = 2; SELECT COUNT(*) FROM information_schema.table_constraints WHERE table_schema = 'trade_db' AND constraint_type = 'CHECK';" | Where-Object { $_ -match '^\d+$' })
    if (($result -join ',') -ne '1,4,2,5,2') { throw "Unexpected trade migration assertion counts: $($result -join ',')." }
    Write-Output "Verified isolated trade migration run $runID."
}
finally {
    if (-not $KeepEnvironment) { docker compose -f $composeFile down -v --remove-orphans | Out-Null }
    $env:COMPOSE_PROJECT_NAME = $previousProject
    $env:MYSQL_PORT = $previousPort
}
