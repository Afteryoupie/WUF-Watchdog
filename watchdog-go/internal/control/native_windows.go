// SPDX-License-Identifier: Apache-2.0
package control

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"syscall"
	"unsafe"
)

var kernel = syscall.NewLazyDLL("kernel32.dll")
var registry = syscall.NewLazyDLL("advapi32.dll")

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

var isolatedKey = regexp.MustCompile(`^Software\\WUFTranslate\\InstallTests\\[0-9a-fA-F-]{36}\\Run$`)

func registryPath(testKey string) (string, error) {
	if testKey == "" {
		return runKey, nil
	}
	if !isolatedKey.MatchString(testKey) {
		return "", errors.New("test registry key must be Software\\WUFTranslate\\InstallTests\\<GUID>\\Run")
	}
	return testKey, nil
}
func openRun(testKey string, writable, create bool) (syscall.Handle, error) {
	path, err := registryPath(testKey)
	if err != nil {
		return 0, err
	}
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
	result, _, _ := registry.NewProc("RegCreateKeyExW").Call(uintptr(syscall.HKEY_CURRENT_USER), uintptr(unsafe.Pointer(wide)), 0, 0, 0, syscall.KEY_QUERY_VALUE|syscall.KEY_SET_VALUE, 0, uintptr(unsafe.Pointer(&key)), 0)
	if result != 0 {
		return 0, syscall.Errno(result)
	}
	return key, nil
}
func readValue(key syscall.Handle) (string, bool, error) {
	name, _ := syscall.UTF16PtrFromString("WUFTranslate")
	var kind, size uint32
	err := syscall.RegQueryValueEx(key, name, nil, &kind, nil, &size)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if kind != syscall.REG_SZ || size%2 != 0 || size > 65536 {
		return "", true, errors.New("WUFTranslate contains an unexpected registry value type or size")
	}
	data := make([]uint16, size/2+1)
	err = syscall.RegQueryValueEx(key, name, nil, &kind, (*byte)(unsafe.Pointer(&data[0])), &size)
	if err != nil {
		return "", false, err
	}
	return syscall.UTF16ToString(data), true, nil
}
func readAutostart(testKey string) (string, bool, error) {
	key, err := openRun(testKey, false, false)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer syscall.RegCloseKey(key)
	return readValue(key)
}
func setAutostart(testKey, command, legacy string, enabled bool) error {
	if command == "" {
		return errors.New("empty startup command")
	}
	key, err := openRun(testKey, true, enabled)
	if !enabled && errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return err
	}
	defer syscall.RegCloseKey(key)
	existing, found, err := readValue(key)
	if err != nil {
		return err
	}
	if found && existing != command && existing != legacy {
		return errors.New("refusing to change another WUFTranslate login entry")
	}
	name, _ := syscall.UTF16PtrFromString("WUFTranslate")
	if !enabled {
		if !found {
			return nil
		}
		result, _, _ := registry.NewProc("RegDeleteValueW").Call(uintptr(key), uintptr(unsafe.Pointer(name)))
		if result != 0 {
			return syscall.Errno(result)
		}
		return nil
	}
	data, err := syscall.UTF16FromString(command)
	if err != nil {
		return err
	}
	result, _, _ := registry.NewProc("RegSetValueExW").Call(uintptr(key), uintptr(unsafe.Pointer(name)), 0, syscall.REG_SZ, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)*2))
	if result != 0 {
		return syscall.Errno(result)
	}
	return nil
}
func executablePath(pid int) (string, error) {
	handle, err := syscall.OpenProcess(0x1000, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer syscall.CloseHandle(handle)
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	result, _, callErr := kernel.NewProc("QueryFullProcessImageNameW").Call(uintptr(handle), 0, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)))
	if result == 0 {
		return "", callErr
	}
	return syscall.UTF16ToString(buffer[:size]), nil
}
func waitForExit(ctx context.Context, pid int) error {
	handle, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, syscall.Errno(87)) {
		return nil
	}
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(handle)
	for {
		result, _, callErr := kernel.NewProc("WaitForSingleObject").Call(uintptr(handle), 50)
		if result == 0 {
			return nil
		}
		if result != 0x102 {
			return fmt.Errorf("cannot wait for owned core: %v", callErr)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}
