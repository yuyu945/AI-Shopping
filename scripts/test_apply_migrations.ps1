$ErrorActionPreference = 'Stop'

$scriptPath = Join-Path $PSScriptRoot 'apply_migrations.ps1'
if (-not (Test-Path -LiteralPath $scriptPath)) {
    throw 'Migration application script is missing.'
}

$source = Get-Content -Raw $scriptPath
if ($source -match '(?i)\$MySQLDsn\b') {
    throw 'Migration application script must not define a MySQLDsn parameter; it must read the DSN from AI_SHOPPING_MYSQL_DSN.'
}
if ($source.Contains('pass -MySQLDsn')) {
    throw 'Migration application script must not document passing a DSN through child process arguments.'
}
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

$malformedDsn = 'not-a-mysql-dsn'
$processStartInfo = [System.Diagnostics.ProcessStartInfo]::new()
$processStartInfo.FileName = 'pwsh'
$processStartInfo.UseShellExecute = $false
$processStartInfo.RedirectStandardOutput = $true
$processStartInfo.RedirectStandardError = $true
$processStartInfo.ArgumentList.Add('-NoProfile')
$processStartInfo.ArgumentList.Add('-File')
$processStartInfo.ArgumentList.Add($scriptPath)
$processStartInfo.Environment['AI_SHOPPING_MYSQL_DSN'] = $malformedDsn
$process = [System.Diagnostics.Process]::Start($processStartInfo)
$stdout = $process.StandardOutput.ReadToEnd()
$stderr = $process.StandardError.ReadToEnd()
$process.WaitForExit()
$output = $stdout + $stderr
if ($process.ExitCode -eq 0) {
    throw 'Malformed MySQL DSN unexpectedly passed migration validation.'
}
if ($output -notmatch 'MySQL DSN must use') {
    throw 'Malformed MySQL DSN was not rejected before Docker access.'
}
if ($output.Contains($malformedDsn)) {
    throw 'Malformed MySQL DSN must not be echoed by migration validation.'
}

Write-Output 'Migration script rejects malformed DSNs without echoing them.'
