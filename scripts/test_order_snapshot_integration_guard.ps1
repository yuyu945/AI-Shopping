$ErrorActionPreference = 'Stop'

$scriptPath = Join-Path $PSScriptRoot 'test_order_snapshot_integration.ps1'
if (-not (Test-Path -LiteralPath $scriptPath)) {
    throw 'Order snapshot integration harness is missing.'
}

$source = Get-Content -Raw $scriptPath
foreach ($forbidden in @('MYSQL_PWD=', '-e "MYSQL_PWD', '-e MYSQL_PASSWORD', '-e MYSQL_ROOT_PASSWORD')) {
    if ($source.Contains($forbidden)) {
        throw "Order snapshot integration harness must not pass credentials through argv: '$forbidden'."
    }
}
foreach ($required in @("AI_SHOPPING_ORDER_SNAPSHOT_INTEGRATION -ne '1'", "`$project = 'm21ordersnapshot'", 'COMPOSE_PROJECT_NAME = $project', "AI_SHOPPING_INTEGRATION_ISOLATED = `$project", "AI_SHOPPING_INTEGRATION_RUN_ID = `$runID", "AI_SHOPPING_MYSQL_DSN =", 'ValidateScript({ $_ -ne 3306 })', 'MySQLPort = 3310', 'finally {', 'docker compose -f $composeFile down -v --remove-orphans', 'Remove-Item "Env:$name"')) {
    if ($source -notmatch [regex]::Escape($required)) {
        throw "Order snapshot integration harness is missing required isolation or cleanup behavior: '$required'."
    }
}

$output = @(& pwsh -NoProfile -File $scriptPath 2>&1)
if ($LASTEXITCODE -ne 0) {
    throw 'Order snapshot integration harness must skip safely without opt-in.'
}
if (($output | Out-String) -notmatch 'SKIP') {
    throw 'Order snapshot integration harness must explicitly report guarded skip.'
}

Write-Output 'Order snapshot integration guard safely skips without opt-in.'
