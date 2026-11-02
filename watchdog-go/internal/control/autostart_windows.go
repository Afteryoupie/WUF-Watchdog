// SPDX-License-Identifier: Apache-2.0
package control

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Use the Task Scheduler COM API so a missing task can be distinguished from
// access failures without parsing localized schtasks error messages. Paths are
// passed as environment data, never interpolated into PowerShell source.
const loginTaskScript = `
$ErrorActionPreference = 'Stop'
try {
 $service = New-Object -ComObject 'Schedule.Service'
 $service.Connect()
 $task = $null
 try { $task = $service.GetFolder('\').GetTask('WUF Watchdog Login') }
 catch {
  if ($_.Exception.HResult -ne -2147024894 -and $_.Exception.InnerException.HResult -ne -2147024894) { throw }
 }
 if ($null -eq $task) { '{"present":false,"enabled":false}'; exit 0 }
 $actions = $task.Definition.Actions
 if ($actions.Count -ne 1) { throw 'Unexpected WUF login task actions; setting unchanged.' }
 $action = $actions.Item(1)
 if ($action.Type -ne 0 -or $action.Path -ine $env:WUF_LOGIN_APP -or $action.Arguments -cne $env:WUF_LOGIN_ARGS) {
  throw 'WUF login task belongs to another installation; setting unchanged.'
 }
 $owner = $task.Definition.Principal.UserId
 $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
 if ($owner -ne $identity.User.Value) {
  $owner = (New-Object Security.Principal.NTAccount($owner)).Translate([Security.Principal.SecurityIdentifier]).Value
 }
 if ($owner -ne $identity.User.Value) { throw 'WUF login task belongs to another user.' }
 if ($env:WUF_LOGIN_ENABLED -ne '') { $task.Enabled = ($env:WUF_LOGIN_ENABLED -eq 'true') }
 @{present=$true; enabled=[bool]$task.Enabled} | ConvertTo-Json -Compress
} catch { [Console]::Error.WriteLine($_.Exception.Message); exit 1 }
`

func loginTask(ctx context.Context, config string, enabled *bool) (bool, bool, error) {
	command := exec.CommandContext(ctx, filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", loginTaskScript)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	value := ""
	if enabled != nil {
		value = fmt.Sprint(*enabled)
	}
	command.Env = append(os.Environ(), "WUF_LOGIN_APP="+filepath.Join(filepath.Dir(config), "app.exe"), "WUF_LOGIN_ARGS=--config \""+config+"\" --quiet", "WUF_LOGIN_ENABLED="+value)
	output, err := command.CombinedOutput()
	if err != nil {
		return false, false, fmt.Errorf("無法存取登入排程：%s (%w)", output, err)
	}
	var state struct {
		Present bool
		Enabled bool
	}
	if err := json.Unmarshal(output, &state); err != nil {
		return false, false, err
	}
	return state.Present, state.Enabled, nil
}

func platformAutostart(ctx context.Context, config string, enabled *bool) (AutostartStatus, error) {
	state := AutostartStatus{Supported: true}
	config, err := filepath.Abs(config)
	if err != nil {
		return state, err
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(config), "app.exe")); err != nil {
		return state, err
	}
	command, err := AutostartCommand(config)
	if err != nil {
		return state, err
	}
	existing, found, err := readAutostart("")
	if err != nil {
		return state, err
	}
	if found && existing != command && existing != legacyNativeCommand(config) {
		return state, fmt.Errorf("登入項目屬於另一個 WUF 安裝，未變更設定")
	}
	present, taskEnabled, err := loginTask(ctx, config, enabled)
	if err != nil {
		return state, err
	}
	if enabled != nil {
		// Existing scheduled installations keep their delay/retry configuration.
		// Other installations use the existing native Run registration.
		if err := setAutostart("", command, legacyNativeCommand(config), *enabled && !present); err != nil {
			return state, err
		}
		_, found, err = readAutostart("")
		if err != nil {
			return state, err
		}
	}
	state.Enabled = found || (present && taskEnabled)
	return state, nil
}
