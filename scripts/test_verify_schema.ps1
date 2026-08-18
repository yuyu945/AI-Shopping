$ErrorActionPreference = 'Stop'

$verifyScript = Join-Path $PSScriptRoot 'verify_schema.ps1'
$output = @(& pwsh -NoProfile -File $verifyScript -MySQLDsn 'not-a-mysql-dsn' -TimeoutSeconds 1 2>&1)

if ($LASTEXITCODE -eq 0) {
    throw 'Malformed MySQL DSN unexpectedly passed schema verification.'
}

$message = ($output | Out-String)
if ($message -notmatch 'MySQL DSN must use') {
    throw 'Malformed MySQL DSN was not rejected before Docker access.'
}

Write-Output 'Malformed MySQL DSN is rejected before Docker access.'
