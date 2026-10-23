# SPDX-License-Identifier: Apache-2.0
[CmdletBinding()]
param(
    [ValidateSet('Install', 'Uninstall', 'EnableAutostart', 'DisableAutostart')][string]$Action = 'Install',
    [string]$PackageDirectory = $PSScriptRoot,
    [string]$TestRegistryRoot
)
$ErrorActionPreference = 'Stop'
$package = [IO.Path]::GetFullPath($PackageDirectory)
$hostName = 'com.wuf_translate.watchdog'
$manifest = Join-Path $package "$hostName.json"
$runCommand = '"' + (Join-Path $package 'app.exe') + '" --quiet'
if ($TestRegistryRoot) {
    if ($TestRegistryRoot -notmatch '^HKCU:\\Software\\WUFTranslate\\InstallTests\\[0-9a-fA-F-]{36}$') { throw 'Test registry root must be a dedicated InstallTests GUID key.' }
    $registryRoot = $TestRegistryRoot
} else { $registryRoot = 'HKCU:\Software' }
$hosts = @(
    "$registryRoot\Google\Chrome\NativeMessagingHosts\$hostName",
    "$registryRoot\Microsoft\Edge\NativeMessagingHosts\$hostName"
)
$runKey = "$registryRoot\Microsoft\Windows\CurrentVersion\Run"
function Read-Value([string]$Path, [string]$Name) {
    if (Test-Path -LiteralPath $Path) { return (Get-Item -LiteralPath $Path).GetValue($Name, $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames) }
    return $null
}
function Remove-Default([string]$Path) {
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($Path.Substring(6), $true)
    if ($null -ne $key) { try { $key.DeleteValue('', $false) } finally { $key.Dispose() } }
}
if ($Action -eq 'Install') {
    foreach ($file in @('app.exe', 'native-host.exe', 'watchdog.exe', 'tray.exe', 'launch.json')) {
        if (-not (Test-Path -LiteralPath (Join-Path $package $file) -PathType Leaf)) { throw "Missing packaged file: $file" }
    }
    foreach ($key in $hosts) {
        $value = Read-Value $key ''
        if ($null -ne $value -and $value -ne $manifest) { throw "Refusing to replace another registration: $key" }
    }
    $body = [ordered]@{
        name = $hostName; description = 'WUF Translate native launcher'; path = (Join-Path $package 'native-host.exe')
        type = 'stdio'; allowed_origins = @('chrome-extension://iecdpfnjngfljdlpookikhidlbfbmidb/')
    } | ConvertTo-Json -Depth 4
    if (Test-Path -LiteralPath $manifest) {
        $existing = Get-Content -LiteralPath $manifest -Raw -Encoding UTF8 | ConvertFrom-Json
        if ($existing.name -ne $hostName -or $existing.path -ne (Join-Path $package 'native-host.exe')) { throw "Refusing to replace another manifest: $manifest" }
    }
    [IO.File]::WriteAllText($manifest, $body, (New-Object Text.UTF8Encoding($false)))
    $added = @()
    try {
        foreach ($key in $hosts) {
            if ($null -eq (Read-Value $key '')) {
                if (-not (Test-Path -LiteralPath $key)) { New-Item -Path $key -Force | Out-Null }
                Set-Item -LiteralPath $key -Value $manifest
                $added += $key
            }
        }
    } catch {
        foreach ($key in $added) { if ((Read-Value $key '') -eq $manifest) { Remove-Default $key } }
        throw
    }
    Write-Output 'Native Host installed for current-user Chrome and Edge. Autostart is unchanged.'
} elseif ($Action -eq 'EnableAutostart') {
    if (-not (Test-Path -LiteralPath (Join-Path $package 'app.exe') -PathType Leaf)) { throw 'Missing app.exe' }
    $value = Read-Value $runKey 'WUFTranslate'
    if ($null -ne $value -and $value -ne $runCommand) { throw 'Refusing to replace another WUFTranslate login entry.' }
    if (-not (Test-Path -LiteralPath $runKey)) { New-Item -Path $runKey -Force | Out-Null }
    New-ItemProperty -LiteralPath $runKey -Name 'WUFTranslate' -Value $runCommand -PropertyType String -Force | Out-Null
    Write-Output 'WUF native app will start at login.'
} else {
    if ($Action -eq 'Uninstall') {
        foreach ($key in $hosts) {
            if ((Read-Value $key '') -eq $manifest) { Remove-Default $key }
        }
    }
    if ((Read-Value $runKey 'WUFTranslate') -eq $runCommand) { Remove-ItemProperty -LiteralPath $runKey -Name 'WUFTranslate' }
    Write-Output 'Matching registrations removed. Package, models, settings, and foreign registrations preserved.'
}
