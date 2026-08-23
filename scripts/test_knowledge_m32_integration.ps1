[CmdletBinding()]
param(
    [ValidateRange(1025, 65535)]
    [ValidateScript({ $_ -ne 3306 })]
    [int]$MySQLPort = 33306,
    [ValidateRange(1025, 65535)]
    [int]$KafkaPort = 29092,
    [ValidateRange(1025, 65535)]
    [int]$MinIOPort = 9100,
    [ValidateRange(1025, 65535)]
    [int]$MinIOConsolePort = 9101,
    [ValidateRange(1025, 65535)]
    [int]$MilvusPort = 19531,
    [ValidateRange(1025, 65535)]
    [int]$MilvusMetricsPort = 19091,
    [switch]$KeepEnvironment
)

$ErrorActionPreference = 'Stop'
if ($env:AI_SHOPPING_KNOWLEDGE_M32_INTEGRATION -ne '1') {
    Write-Output 'SKIP: set AI_SHOPPING_KNOWLEDGE_M32_INTEGRATION=1 to run the isolated M3.2 knowledge integration test.'
    exit 0
}
if ([string]::IsNullOrWhiteSpace($env:MYSQL_PASSWORD) -or [string]::IsNullOrWhiteSpace($env:MYSQL_ROOT_PASSWORD)) {
    throw 'MYSQL_PASSWORD and MYSQL_ROOT_PASSWORD must be set in the process environment.'
}
if ([string]::IsNullOrWhiteSpace($env:AI_SHOPPING_DASHSCOPE_API_KEY)) {
    throw 'AI_SHOPPING_DASHSCOPE_API_KEY must be set in the process environment.'
}

$project = 'm32knowledge'
$mysqlContainer = "$project-mysql-1"
$runID = [guid]::NewGuid().ToString()
$composeFile = 'deploy/docker-compose.yml'
$tracked = @(
    'COMPOSE_PROJECT_NAME',
    'MYSQL_PORT',
    'KAFKA_PORT',
    'MINIO_PORT',
    'MINIO_CONSOLE_PORT',
    'MILVUS_PORT',
    'MILVUS_METRICS_PORT',
    'MINIO_ACCESS_KEY',
    'MINIO_SECRET_KEY',
    'AI_SHOPPING_INTEGRATION',
    'AI_SHOPPING_INTEGRATION_ISOLATED',
    'AI_SHOPPING_INTEGRATION_RUN_ID',
    'AI_SHOPPING_MYSQL_DSN',
    'AI_SHOPPING_MINIO_ENDPOINT',
    'AI_SHOPPING_MINIO_ACCESS_KEY',
    'AI_SHOPPING_MINIO_SECRET_KEY',
    'AI_SHOPPING_MINIO_BUCKET',
    'AI_SHOPPING_KAFKA_BROKERS',
    'AI_SHOPPING_MILVUS_ADDRESS'
)
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
    $env:KAFKA_PORT = $KafkaPort.ToString()
    $env:MINIO_PORT = $MinIOPort.ToString()
    $env:MINIO_CONSOLE_PORT = $MinIOConsolePort.ToString()
    $env:MILVUS_PORT = $MilvusPort.ToString()
    $env:MILVUS_METRICS_PORT = $MilvusMetricsPort.ToString()
    if ([string]::IsNullOrWhiteSpace($env:MINIO_ACCESS_KEY)) { $env:MINIO_ACCESS_KEY = 'm32-knowledge-minio-access' }
    if ([string]::IsNullOrWhiteSpace($env:MINIO_SECRET_KEY)) { $env:MINIO_SECRET_KEY = 'm32-knowledge-minio-secret' }

    docker compose -f $composeFile up -d mysql kafka kafka-init minio etcd milvus | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not start isolated knowledge Compose services.' }

    $containers = @(
        "$project-mysql-1",
        "$project-kafka-1",
        "$project-minio-1",
        "$project-etcd-1",
        "$project-milvus-1"
    )
    $deadline = (Get-Date).AddSeconds(180)
    foreach ($container in $containers) {
        do {
            $health = (docker inspect --format '{{.State.Health.Status}}' $container 2>$null).Trim()
            if ($health -eq 'healthy') { break }
            if ((Get-Date) -ge $deadline) { throw "Container $container did not become healthy within 180 seconds." }
            Start-Sleep -Seconds 2
        } while ($true)
    }

    & "$PSScriptRoot\prepare_knowledge_integration.ps1" -RunID $runID -Container $mysqlContainer
    if ($LASTEXITCODE -ne 0) { throw 'Knowledge integration guard preparation failed.' }

    $env:AI_SHOPPING_INTEGRATION = '1'
    $env:AI_SHOPPING_INTEGRATION_ISOLATED = $project
    $env:AI_SHOPPING_INTEGRATION_RUN_ID = $runID
    $env:AI_SHOPPING_MYSQL_DSN = "app:$($env:MYSQL_PASSWORD)@tcp(127.0.0.1:$MySQLPort)/knowledge_db?parseTime=true"
    $env:AI_SHOPPING_MINIO_ENDPOINT = "127.0.0.1:$MinIOPort"
    $env:AI_SHOPPING_MINIO_ACCESS_KEY = $env:MINIO_ACCESS_KEY
    $env:AI_SHOPPING_MINIO_SECRET_KEY = $env:MINIO_SECRET_KEY
    $env:AI_SHOPPING_MINIO_BUCKET = 'knowledge'
    $env:AI_SHOPPING_KAFKA_BROKERS = "127.0.0.1:$KafkaPort"
    $env:AI_SHOPPING_MILVUS_ADDRESS = "http://127.0.0.1:$MilvusPort"

    go test -tags=integration ./services/knowledge-service/internal/knowledge -run '^TestKnowledgeM32DependencyIntegration$' -count=1 -v
    if ($LASTEXITCODE -ne 0) { throw 'M3.2 knowledge integration test failed.' }
    Write-Output "Verified isolated M3.2 knowledge integration run $runID."
}
catch {
    $primaryFailure = $_
}
finally {
    if (-not $KeepEnvironment) {
        try {
            docker compose -f $composeFile down -v --remove-orphans | Out-Null
            if ($LASTEXITCODE -ne 0) { throw 'Isolated knowledge Compose cleanup failed.' }
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
if ($null -ne $cleanupFailure) { throw 'Isolated knowledge Compose cleanup failed.' }
