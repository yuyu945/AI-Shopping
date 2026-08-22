$ErrorActionPreference = 'Stop'

$scriptPath = Join-Path $PSScriptRoot 'test_trade_migration_integration.ps1'
if (-not (Test-Path -LiteralPath $scriptPath)) {
    throw 'Trade migration integration script is missing.'
}

$source = Get-Content -Raw $scriptPath
foreach ($forbidden in @('MYSQL_PWD=', '-e "MYSQL_PWD')) {
    if ($source.Contains($forbidden)) {
        throw "Trade migration integration script must not pass credentials through docker compose argv: '$forbidden'."
    }
}
if ($source -match 'docker compose[^\r\n]*\s-e\s+[^\r\n]*MYSQL_(PASSWORD|ROOT_PASSWORD)') {
    throw 'Trade migration integration script must not pass a Compose password environment variable through docker compose argv.'
}
if ($source -notmatch [regex]::Escape('MYSQL_ROOT_PASSWORD')) {
    throw 'Trade migration integration script must use the Compose-managed root password inside the MySQL container.'
}

$output = @(& pwsh -NoProfile -File $scriptPath 2>&1)
if ($LASTEXITCODE -ne 0) {
    throw 'Integration script must safely skip when its opt-in guard is absent.'
}
if (($output | Out-String) -notmatch 'SKIP') {
    throw 'Integration script must explicitly report a guarded skip.'
}

Write-Output 'Trade migration integration guard safely skips without opt-in.'
