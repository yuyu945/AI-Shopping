$ErrorActionPreference = 'Stop'

$scriptPath = Join-Path $PSScriptRoot 'apply_catalog_migrations.ps1'
if (-not (Test-Path -LiteralPath $scriptPath)) { throw 'Catalog migration application script is missing.' }
$source = Get-Content -Raw $scriptPath
foreach ($required in @('catalog_db', 'schema_migrations', 'Get-ChildItem', 'Sort-Object', 'already applied', '--password="$MYSQL_PASSWORD"')) {
    if ($source -notmatch [regex]::Escape($required)) { throw "Catalog migration application script must contain '$required'." }
}
foreach ($forbidden in @('MYSQL_PWD=', '-e "MYSQL_PWD', '$connection.Password', '$MySQLDsn')) {
    if ($source.Contains($forbidden)) { throw "Catalog migration application script must not expose credentials or accept DSNs through argv: '$forbidden'." }
}
if ($source -match 'Write-(Output|Host|Verbose).*MySQLDsn') { throw 'Catalog migration application script must not echo the MySQL DSN.' }

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
$output = $process.StandardOutput.ReadToEnd() + $process.StandardError.ReadToEnd()
$process.WaitForExit()
if ($process.ExitCode -eq 0 -or $output -notmatch 'MySQL DSN must use' -or $output.Contains($malformedDsn)) { throw 'Catalog migration script must reject malformed DSNs without echoing them.' }

Write-Output 'Catalog migration script rejects malformed DSNs without echoing them.'
