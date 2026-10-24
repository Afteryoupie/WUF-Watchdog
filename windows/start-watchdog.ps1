param([switch]$Silent)
$ErrorActionPreference = 'Stop'
$tool = Join-Path (Split-Path $PSScriptRoot -Parent) 'runtime\windows\wuf-tool.exe'
& $tool control --action start
if ($LASTEXITCODE -ne 0) { throw 'WUF Watchdog startup failed.' }
