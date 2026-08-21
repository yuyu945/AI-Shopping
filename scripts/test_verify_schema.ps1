$ErrorActionPreference = 'Stop'

$verifyScript = Join-Path $PSScriptRoot 'verify_schema.ps1'
$verifySource = Get-Content -Raw $verifyScript

if ($verifySource -notmatch 'exec -T -e "MYSQL_PWD=\$\(\$connection\.Password\)"') {
    throw 'Schema verification does not pass the DSN password through docker compose exec environment.'
}

if ($verifySource -match '\$connection\.Password \| docker compose') {
    throw 'Schema verification still pipes the DSN password through docker compose exec stdin.'
}

$output = @(& pwsh -NoProfile -File $verifyScript -MySQLDsn 'not-a-mysql-dsn' -TimeoutSeconds 1 2>&1)

if ($LASTEXITCODE -eq 0) {
    throw 'Malformed MySQL DSN unexpectedly passed schema verification.'
}

$message = ($output | Out-String)
if ($message -notmatch 'MySQL DSN must use') {
    throw 'Malformed MySQL DSN was not rejected before Docker access.'
}

Write-Output 'Malformed MySQL DSN is rejected before Docker access.'
