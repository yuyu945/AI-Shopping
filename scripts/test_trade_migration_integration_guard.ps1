$ErrorActionPreference = 'Stop'

$scriptPath = Join-Path $PSScriptRoot 'test_trade_migration_integration.ps1'
if (-not (Test-Path -LiteralPath $scriptPath)) {
    throw 'Trade migration integration script is missing.'
}

$output = @(& pwsh -NoProfile -File $scriptPath 2>&1)
if ($LASTEXITCODE -ne 0) {
    throw 'Integration script must safely skip when its opt-in guard is absent.'
}
if (($output | Out-String) -notmatch 'SKIP') {
    throw 'Integration script must explicitly report a guarded skip.'
}

Write-Output 'Trade migration integration guard safely skips without opt-in.'
