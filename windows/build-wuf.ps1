# SPDX-License-Identifier: Apache-2.0
[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)][string]$GoExecutable,
    [Parameter(Mandatory=$true)][string]$OutputDirectory,
    [switch]$WorkspaceLayout
)
$ErrorActionPreference = 'Stop'
$repo = Split-Path $PSScriptRoot -Parent
$output = [IO.Path]::GetFullPath($OutputDirectory)
if (Test-Path -LiteralPath $output) { throw "Output already exists: $output" }
if (-not [IO.Path]::IsPathRooted($GoExecutable) -or -not (Test-Path -LiteralPath $GoExecutable -PathType Leaf)) { throw 'Specify the absolute Go executable path.' }
$pkgOutput = if ($WorkspaceLayout) { Join-Path $output 'Watchdog' } else { $output }
New-Item -ItemType Directory -Path "$pkgOutput\resources\icons" -Force | Out-Null
$priorOS, $priorArch, $priorCGO = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED
try {
    $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
    foreach ($entry in @('watchdog', 'native-host', 'tray', 'app', 'wuf-tool')) {
        $buildArgs = @('-C', "$repo\watchdog-go", 'build', '-trimpath', '-buildvcs=false')
        if ($entry -in @('tray', 'app')) { $buildArgs += @('-ldflags', '-H=windowsgui') }
        $buildArgs += @('-o', "$pkgOutput\$entry.exe", "./cmd/$entry")
        & $GoExecutable @buildArgs
        if ($LASTEXITCODE -ne 0) { throw "Go build failed: $entry" }
    }
    Copy-Item -LiteralPath "$repo\watchdog\templates\dashboard.html" -Destination "$pkgOutput\resources\dashboard.html"
    foreach ($state in @('offline', 'ready', 'running', 'sleeping', 'starting')) {
        Copy-Item -LiteralPath "$repo\watchdog\assets\watchdog-$state.ico" -Destination "$pkgOutput\resources\icons"
    }
    $config = [ordered]@{
        core = 'watchdog.exe'; tray = 'tray.exe'; base_url = 'http://127.0.0.1:8080'
        arguments = @('--dashboard', 'resources/dashboard.html', '--models-dir', '../Models', '--llama', '../llama.cpp/llama-server.exe')
        tray_arguments = @('--config', 'launch.json', '--assets', 'resources/icons')
    }
    $utf8NoBom = New-Object Text.UTF8Encoding($false)
    [IO.File]::WriteAllText("$pkgOutput\launch.json", ($config | ConvertTo-Json -Depth 5), $utf8NoBom)
    Copy-Item -LiteralPath "$PSScriptRoot\install-wuf.ps1" -Destination $pkgOutput
    Copy-Item -LiteralPath "$repo\watchdog-go\LICENSE" -Destination "$pkgOutput\LICENSE"
    $goroot = & $GoExecutable env GOROOT
    if ($LASTEXITCODE -ne 0) { throw 'Cannot locate Go runtime license.' }
    Copy-Item -LiteralPath (Join-Path $goroot 'LICENSE') -Destination "$pkgOutput\GO-LICENSE.txt"
    [IO.File]::WriteAllText("$pkgOutput\NOTICE.txt", "WUF native package: first-party Apache-2.0 code; Go runtime BSD-3-Clause (GO-LICENSE.txt). Model weights and inference engines are supplied separately. The legacy extension is not included.`r`n", $utf8NoBom)
    if ($WorkspaceLayout) {
        $llamaDir = Join-Path $output 'llama.cpp'
        $modelsDir = Join-Path $output 'Models'
        New-Item -ItemType Directory -Path $llamaDir, $modelsDir -Force | Out-Null
        [IO.File]::WriteAllText(
            (Join-Path $llamaDir 'README.txt'),
            "請從 https://github.com/ggml-org/llama.cpp/releases 下載 Windows x64 套件（例如 bin-win-cpu-x64.zip），`r`n將其中的 llama-server.exe 與同包 DLL 全部解壓縮到這個 llama.cpp 資料夾內（確認路徑為 llama.cpp\llama-server.exe）。`r`n",
            $utf8NoBom
        )
        [IO.File]::WriteAllText(
            (Join-Path $modelsDir 'README.txt'),
            "請將下載的 .gguf 模型檔直接放入這個 Models 資料夾（例如 Qwen3-0.6B-Q8_0.gguf），`r`n或為每個模型建立一層子資料夾後放入其中。完成後執行 Watchdog\app.exe 即可使用。`r`n",
            $utf8NoBom
        )
    }
    Write-Output "Built Windows amd64 package: $output"
} finally {
    $env:GOOS = $priorOS; $env:GOARCH = $priorArch; $env:CGO_ENABLED = $priorCGO
}