// SPDX-License-Identifier: Apache-2.0
//go:build windows

package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"wuf-translate/watchdog-go/internal/launch"
)

var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	regCreateKeyEx = advapi32.NewProc("RegCreateKeyExW")
	regSetValueEx  = advapi32.NewProc("RegSetValueExW")
	regDeleteValue = advapi32.NewProc("RegDeleteValueW")
)

const (
	winRunKey = `Software\Microsoft\Windows\CurrentVersion\Run`
)

var winNativeHostKeys = []string{
	`Software\Google\Chrome\NativeMessagingHosts\` + launch.HostName,
	`Software\Microsoft\Edge\NativeMessagingHosts\` + launch.HostName,
}

func isPackagedDir(dir string) bool {
	for _, name := range []string{"app.exe", "native-host.exe", "watchdog.exe", "tray.exe", "launch.json"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	return true
}

func openRegKey(path string, writable, create bool) (syscall.Handle, error) {
	wide, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var key syscall.Handle
	if !create {
		access := uint32(syscall.KEY_QUERY_VALUE)
		if writable {
			access |= syscall.KEY_SET_VALUE
		}
		err = syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, wide, 0, access, &key)
		return key, err
	}
	res, _, _ := regCreateKeyEx.Call(
		uintptr(syscall.HKEY_CURRENT_USER),
		uintptr(unsafe.Pointer(wide)),
		0, 0, 0,
		uintptr(syscall.KEY_QUERY_VALUE|syscall.KEY_SET_VALUE),
		0,
		uintptr(unsafe.Pointer(&key)),
		0,
	)
	if res != 0 {
		return 0, syscall.Errno(res)
	}
	return key, nil
}

func readRegString(path, valueName string) (string, bool, error) {
	key, err := openRegKey(path, false, false)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer syscall.RegCloseKey(key)

	var namePtr *uint16
	if valueName != "" {
		namePtr, err = syscall.UTF16PtrFromString(valueName)
		if err != nil {
			return "", false, err
		}
	}
	var kind, size uint32
	err = syscall.RegQueryValueEx(key, namePtr, nil, &kind, nil, &size)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if kind != syscall.REG_SZ || size%2 != 0 || size > 65536 {
		return "", true, errors.New("unexpected registry value type or size")
	}
	buf := make([]uint16, size/2+1)
	if size > 0 {
		err = syscall.RegQueryValueEx(key, namePtr, nil, &kind, (*byte)(unsafe.Pointer(&buf[0])), &size)
		if err != nil {
			return "", false, err
		}
	}
	return syscall.UTF16ToString(buf), true, nil
}

func writeRegString(path, valueName, value string) error {
	key, err := openRegKey(path, true, true)
	if err != nil {
		return err
	}
	defer syscall.RegCloseKey(key)

	var namePtr *uint16
	if valueName != "" {
		namePtr, err = syscall.UTF16PtrFromString(valueName)
		if err != nil {
			return err
		}
	}
	data, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	res, _, _ := regSetValueEx.Call(
		uintptr(key),
		uintptr(unsafe.Pointer(namePtr)),
		0,
		syscall.REG_SZ,
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)*2),
	)
	if res != 0 {
		return syscall.Errno(res)
	}
	return nil
}

func deleteRegValue(path, valueName string) error {
	key, err := openRegKey(path, true, false)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return err
	}
	defer syscall.RegCloseKey(key)

	var namePtr *uint16
	if valueName != "" {
		namePtr, err = syscall.UTF16PtrFromString(valueName)
		if err != nil {
			return err
		}
	}
	res, _, _ := regDeleteValue.Call(uintptr(key), uintptr(unsafe.Pointer(namePtr)))
	if res != 0 && !errors.Is(syscall.Errno(res), syscall.ERROR_FILE_NOT_FOUND) {
		return syscall.Errno(res)
	}
	return nil
}

func expectedAutostartCommands(dir string) (primary, legacy string) {
	app := filepath.Join(dir, "app.exe")
	cfg := filepath.Join(dir, "launch.json")
	return `"` + app + `" --config "` + cfg + `" --quiet`, `"` + app + `" --quiet`
}

func manifestPayload(dir string) ([]byte, string, error) {
	nativeHost := filepath.Join(dir, "native-host.exe")
	manifestPath := filepath.Join(dir, launch.HostName+".json")
	body := map[string]any{
		"name":            launch.HostName,
		"description":     "WUF Watchdog native launcher",
		"path":            nativeHost,
		"type":            "stdio",
		"allowed_origins": []string{launch.ExtensionOrigin},
	}
	data, err := json.MarshalIndent(body, "", "  ")
	return data, manifestPath, err
}

func manifestMatches(manifestPath, nativeHost string) (exists bool, match bool, err error) {
	raw, err := os.ReadFile(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return true, false, err
	}
	var parsed struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return true, false, err
	}
	ok := parsed.Name == launch.HostName && strings.EqualFold(filepath.Clean(parsed.Path), filepath.Clean(nativeHost))
	return true, ok, nil
}

func ensureNativeHost(exePath string) error {
	dir := filepath.Dir(exePath)
	if !isPackagedDir(dir) {
		return nil
	}
	data, manifestPath, err := manifestPayload(dir)
	if err != nil {
		return err
	}
	nativeHost := filepath.Join(dir, "native-host.exe")
	exists, match, err := manifestMatches(manifestPath, nativeHost)
	if err != nil {
		return fmt.Errorf("refusing foreign manifest %s: %w", manifestPath, err)
	}
	if exists && !match {
		return fmt.Errorf("refusing foreign manifest: %s", manifestPath)
	}
	for _, key := range winNativeHostKeys {
		val, found, err := readRegString(key, "")
		if err != nil {
			return err
		}
		if found && !strings.EqualFold(filepath.Clean(val), filepath.Clean(manifestPath)) {
			return fmt.Errorf("refusing foreign registration: %s", key)
		}
	}
	if !exists {
		if err := os.WriteFile(manifestPath, data, 0644); err != nil {
			return err
		}
	}
	for _, key := range winNativeHostKeys {
		_, found, err := readRegString(key, "")
		if err != nil {
			return err
		}
		if !found {
			if err := writeRegString(key, "", manifestPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func setAutostart(exePath string, enabled bool) error {
	dir := filepath.Dir(exePath)
	if !isPackagedDir(dir) {
		return errors.New("autostart requires running from the packaged Watchdog directory")
	}
	primary, legacy := expectedAutostartCommands(dir)
	val, found, err := readRegString(winRunKey, "WUFTranslate")
	if err != nil {
		return err
	}
	if found && val != primary && val != legacy {
		return errors.New("refusing to change another WUFTranslate login entry")
	}
	if !enabled {
		if !found {
			return nil
		}
		return deleteRegValue(winRunKey, "WUFTranslate")
	}
	return writeRegString(winRunKey, "WUFTranslate", primary)
}

func uninstall(exePath string) error {
	dir := filepath.Dir(exePath)
	if !isPackagedDir(dir) {
		return errors.New("uninstall requires running from the packaged Watchdog directory")
	}
	manifestPath := filepath.Join(dir, launch.HostName+".json")
	for _, key := range winNativeHostKeys {
		val, found, err := readRegString(key, "")
		if err != nil {
			return err
		}
		if found && strings.EqualFold(filepath.Clean(val), filepath.Clean(manifestPath)) {
			if err := deleteRegValue(key, ""); err != nil {
				return err
			}
		}
	}
	primary, legacy := expectedAutostartCommands(dir)
	val, found, err := readRegString(winRunKey, "WUFTranslate")
	if err != nil {
		return err
	}
	if found && (val == primary || val == legacy) {
		if err := deleteRegValue(winRunKey, "WUFTranslate"); err != nil {
			return err
		}
	}
	return nil
}

func queryPlatformStatus(_ context.Context, exePath, _ string) Status {
	dir := filepath.Dir(exePath)
	if !isPackagedDir(dir) {
		return Status{
			NativeHost: "unsupported",
			Autostart:  "unsupported",
			FMStatus:   "not_applicable",
		}
	}
	manifestPath := filepath.Join(dir, launch.HostName+".json")
	nativeState := "installed"
	nativeDetail := ""
	allInstalled := true
	for _, key := range winNativeHostKeys {
		val, found, err := readRegString(key, "")
		if err != nil {
			nativeState = "foreign"
			nativeDetail = err.Error()
			allInstalled = false
			break
		}
		if found && !strings.EqualFold(filepath.Clean(val), filepath.Clean(manifestPath)) {
			nativeState = "foreign"
			nativeDetail = key
			allInstalled = false
			break
		}
		if !found {
			allInstalled = false
		}
	}
	if nativeState == "installed" && !allInstalled {
		nativeState = "not_installed"
	} else if nativeState == "installed" {
		exists, match, err := manifestMatches(manifestPath, filepath.Join(dir, "native-host.exe"))
		if err != nil || (exists && !match) {
			nativeState = "foreign"
			nativeDetail = manifestPath
		} else if !exists {
			nativeState = "not_installed"
		}
	}

	autostartState := "disabled"
	autostartDetail := ""
	primary, legacy := expectedAutostartCommands(dir)
	val, found, err := readRegString(winRunKey, "WUFTranslate")
	if err != nil {
		autostartState = "foreign"
		autostartDetail = err.Error()
	} else if found {
		if val == primary || val == legacy {
			autostartState = "enabled"
		} else {
			autostartState = "foreign"
			autostartDetail = val
		}
	}

	return Status{
		NativeHost:       nativeState,
		NativeHostDetail: nativeDetail,
		Autostart:        autostartState,
		AutostartDetail:  autostartDetail,
		FMStatus:         "not_applicable",
	}
}
