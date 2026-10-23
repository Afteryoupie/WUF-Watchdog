# SPDX-License-Identifier: Apache-2.0
[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)][string]$GoExecutable,
    [Parameter(Mandatory=$true)][string]$OutputDirectory
)
$ErrorActionPreference = 'Stop'
$repo = Split-Path $PSScriptRoot -Parent
$output = [IO.Path]::GetFullPath($OutputDirectory)
if (Test-Path -LiteralPath $output) { throw "Output already exists: $output" }
if (-not [IO.Path]::IsPathRooted($GoExecutable) -or -not (Test-Path -LiteralPath $GoExecutable -PathType Leaf)) { throw 'Specify the absolute Go executable path.' }
New-Item -ItemType Directory -Path "$output\resources\icons", "$output\resources\laya\watchdog" -Force | Out-Null
$priorOS, $priorArch, $priorCGO = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED
try {
    $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
    foreach ($entry in @('watchdog', 'native-host', 'tray', 'app', 'wuf-tool')) {
        $buildArgs = @('-C', "$repo\watchdog-go", 'build', '-trimpath', '-buildvcs=false')
        if ($entry -in @('tray', 'app')) { $buildArgs += @('-ldflags', '-H=windowsgui') }
        $buildArgs += @('-o', "$output\$entry.exe", "./cmd/$entry")
        & $GoExecutable @buildArgs
        if ($LASTEXITCODE -ne 0) { throw "Go build failed: $entry" }
    }
    Copy-Item -LiteralPath "$repo\watchdog\templates\dashboard.html" -Destination "$output\resources\dashboard.html"
    foreach ($state in @('offline', 'ready', 'running', 'sleeping', 'starting')) {
        Copy-Item -LiteralPath "$repo\watchdog\assets\watchdog-$state.ico" -Destination "$output\resources\icons"
    }
    foreach ($file in @('__init__.py', 'decision_protocol.py', 'laya_backend.py')) {
        Copy-Item -LiteralPath "$repo\watchdog\$file" -Destination "$output\resources\laya\watchdog"
    }
    $config = [ordered]@{
        core = 'watchdog.exe'; tray = 'tray.exe'; base_url = 'http://127.0.0.1:8080'
        arguments = @('--dashboard', 'resources/dashboard.html', '--laya-root', 'resources/laya')
        tray_arguments = @('--config', 'launch.json', '--assets', 'resources/icons')
    }
    [IO.File]::WriteAllText("$output\launch.json", ($config | ConvertTo-Json -Depth 5), (New-Object Text.UTF8Encoding($false)))
    Copy-Item -LiteralPath "$PSScriptRoot\install-wuf.ps1" -Destination $output
    Copy-Item -LiteralPath "$repo\watchdog-go\LICENSE" -Destination "$output\LICENSE"
    $goroot = & $GoExecutable env GOROOT
    if ($LASTEXITCODE -ne 0) { throw 'Cannot locate Go runtime license.' }
    Copy-Item -LiteralPath (Join-Path $goroot 'LICENSE') -Destination "$output\GO-LICENSE.txt"
    [IO.File]::WriteAllText("$output\NOTICE.txt", "WUF native package: first-party Apache-2.0 code; Go runtime BSD-3-Clause (GO-LICENSE.txt). Model weights and inference engines are supplied separately. The legacy extension is not included.`r`n")
    Write-Output "Built Windows amd64 package: $output"
} finally {
    $env:GOOS = $priorOS; $env:GOARCH = $priorArch; $env:CGO_ENABLED = $priorCGO
}
