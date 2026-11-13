// SPDX-License-Identifier: Apache-2.0
package setup

import (
	"context"
	"os"
	"runtime"
)

const (
	LoginLabel = "local.wuf.translate.watchdog"
)

type Status struct {
	Platform         string `json:"platform"`
	NativeHost       string `json:"native_host"`
	NativeHostDetail string `json:"native_host_detail,omitempty"`
	Autostart        string `json:"autostart"`
	AutostartDetail  string `json:"autostart_detail,omitempty"`
	FMStatus         string `json:"fm_status"`
	FMMessage        string `json:"fm_message,omitempty"`
}

func currentExe() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

// EnsureNativeHost registers the current-user browser Native Messaging host when
// running from a packaged bundle/directory, without overwriting foreign entries.
func EnsureNativeHost(exePath string) error {
	if exePath == "" {
		exePath = currentExe()
	}
	return ensureNativeHost(exePath)
}

// QueryStatus inspects Native Host registration, login autostart, and macOS fm readiness.
func QueryStatus(ctx context.Context, exePath, fmBin string) Status {
	if exePath == "" {
		exePath = currentExe()
	}
	st := queryPlatformStatus(ctx, exePath, fmBin)
	st.Platform = runtime.GOOS
	return st
}

// SetAutostart enables or disables login autostart for the current user.
func SetAutostart(exePath string, enabled bool) error {
	if exePath == "" {
		exePath = currentExe()
	}
	return setAutostart(exePath, enabled)
}

// Uninstall removes matching current-user Native Host and login autostart entries.
func Uninstall(exePath string) error {
	if exePath == "" {
		exePath = currentExe()
	}
	return uninstall(exePath)
}
