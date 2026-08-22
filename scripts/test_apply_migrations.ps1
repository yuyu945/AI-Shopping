$ErrorActionPreference = 'Stop'

$scriptPath = Join-Path $PSScriptRoot 'apply_migrations.ps1'
if (-not (Test-Path -LiteralPath $scriptPath)) {
    throw 'Migration application script is missing.'
}

$source = Get-Content -Raw $scriptPath
foreach ($required in @('schema_migrations', 'Get-ChildItem', 'Sort-Object', 'already applied')) {
    if ($source -notmatch [regex]::Escape($required)) {
        throw "Migration application script must contain '$required'."
    }
}
foreach ($forbidden in @('MYSQL_PWD=', '-e "MYSQL_PWD', '$connection.Password')) {
    if ($source.Contains($forbidden)) {
        throw "Migration application script must not pass credentials through docker compose argv: '$forbidden'."
    }
}
if ($source -notmatch [regex]::Escape('MYSQL_PASSWORD')) {
    throw 'Migration application script must use the Compose-managed app password inside the MySQL container.'
}
foreach ($required in @('exec -T mysql sh -c', '--password="$MYSQL_PASSWORD"', '$queryHost = if', '$queryPort = if', "'3306'")) {
    if ($source -notmatch [regex]::Escape($required)) {
        throw "Migration application script must preserve '$required'."
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
