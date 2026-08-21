$ErrorActionPreference = 'Stop'

$scriptPath = Join-Path $PSScriptRoot 'apply_migrations.ps1'
if (-not (Test-Path -LiteralPath $scriptPath)) {
    throw 'Migration application script is missing.'
}

$source = Get-Content -Raw $scriptPath
foreach ($required in @('schema_migrations', 'Get-ChildItem', 'Sort-Object', 'MYSQL_PWD=', 'already applied')) {
    if ($source -notmatch [regex]::Escape($required)) {
        throw "Migration application script must contain '$required'."
    }
}
if ($source -match 'Write-(Output|Host|Verbose).*MySQLDsn') {
    throw 'Migration application script must not echo the MySQL DSN.'
}

$output = @(& pwsh -NoProfile -File $scriptPath -MySQLDsn 'not-a-mysql-dsn' 2>&1)
if ($LASTEXITCODE -eq 0) {
    throw 'Malformed MySQL DSN unexpectedly passed migration validation.'
}
if (($output | Out-String) -notmatch 'MySQL DSN must use') {
    throw 'Malformed MySQL DSN was not rejected before Docker access.'
}

Write-Output 'Migration script rejects malformed DSNs without echoing them.'
