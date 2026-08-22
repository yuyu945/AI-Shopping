[CmdletBinding()]
param(
    [ValidateRange(1025, 65535)]
    [ValidateScript({ $_ -ne 3306 })]
    [int]$MySQLPort = 3310,
    [switch]$KeepEnvironment
)

$ErrorActionPreference = 'Stop'
if ($env:AI_SHOPPING_ORDER_SNAPSHOT_INTEGRATION -ne '1') {
    Write-Output 'SKIP: set AI_SHOPPING_ORDER_SNAPSHOT_INTEGRATION=1 to run the isolated order snapshot integration test.'
    exit 0
}
if ([string]::IsNullOrWhiteSpace($env:MYSQL_PASSWORD) -or [string]::IsNullOrWhiteSpace($env:MYSQL_ROOT_PASSWORD)) {
    throw 'MYSQL_PASSWORD and MYSQL_ROOT_PASSWORD must be set in the process environment.'
}

$project = 'm21ordersnapshot'
$container = "$project-mysql-1"
$runID = [guid]::NewGuid().ToString()
$composeFile = 'deploy/docker-compose.yml'
$tracked = @('COMPOSE_PROJECT_NAME', 'MYSQL_PORT', 'AI_SHOPPING_INTEGRATION', 'AI_SHOPPING_INTEGRATION_ISOLATED', 'AI_SHOPPING_INTEGRATION_RUN_ID', 'AI_SHOPPING_MYSQL_DSN', 'MINIO_ACCESS_KEY', 'MINIO_SECRET_KEY')
$previous = @{}
foreach ($name in $tracked) {
    $previous[$name] = [pscustomobject]@{
        Exists = Test-Path "Env:$name"
        Value = [Environment]::GetEnvironmentVariable($name, 'Process')
    }
}

$primaryFailure = $null
$cleanupFailure = $null
try {
    $env:COMPOSE_PROJECT_NAME = $project
    $env:MYSQL_PORT = $MySQLPort.ToString()
    # Compose interpolates all services even when starting only mysql. These
    # process-local placeholders are never used by the mysql-only project.
    if ([string]::IsNullOrWhiteSpace($env:MINIO_ACCESS_KEY)) { $env:MINIO_ACCESS_KEY = 'm21-order-integration-minio-access' }
    if ([string]::IsNullOrWhiteSpace($env:MINIO_SECRET_KEY)) { $env:MINIO_SECRET_KEY = 'm21-order-integration-minio-secret' }
    docker compose -f $composeFile up -d mysql | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not start isolated MySQL Compose service.' }

    $deadline = (Get-Date).AddSeconds(90)
    do {
        $health = (docker inspect --format '{{.State.Health.Status}}' $container 2>$null).Trim()
        if ($health -eq 'healthy') { break }
        if ((Get-Date) -ge $deadline) { throw 'Isolated MySQL did not become healthy within 90 seconds.' }
        Start-Sleep -Seconds 2
    } while ($true)

    & "$PSScriptRoot\prepare_order_snapshot_integration.ps1" -RunID $runID -Container $container
    if ($LASTEXITCODE -ne 0) { throw 'Order snapshot integration guard preparation failed.' }

    $env:AI_SHOPPING_INTEGRATION = '1'
    $env:AI_SHOPPING_INTEGRATION_ISOLATED = $project
    $env:AI_SHOPPING_INTEGRATION_RUN_ID = $runID
    $env:AI_SHOPPING_MYSQL_DSN = "app:$($env:MYSQL_PASSWORD)@tcp(127.0.0.1:$MySQLPort)/trade_db"
    go test -tags=integration ./services/order-service/internal/order -run '^TestOrderSnapshotMySQLIntegration$' -count=1 -v
    if ($LASTEXITCODE -ne 0) { throw 'Order snapshot integration test failed.' }
    Write-Output "Verified isolated order snapshot integration run $runID."
}
catch {
    $primaryFailure = $_
}
finally {
    if (-not $KeepEnvironment) {
        try {
            docker compose -f $composeFile down -v --remove-orphans | Out-Null
            if ($LASTEXITCODE -ne 0) { throw 'Isolated MySQL Compose cleanup failed.' }
        }
        catch {
            $cleanupFailure = $_
        }
    }
    foreach ($name in $tracked) {
        if ($previous[$name].Exists) {
            Set-Item "Env:$name" $previous[$name].Value
        }
        else {
            Remove-Item "Env:$name" -ErrorAction SilentlyContinue
        }
    }
}
if ($null -ne $primaryFailure) { throw $primaryFailure }
if ($null -ne $cleanupFailure) { throw 'Isolated MySQL Compose cleanup failed.' }
