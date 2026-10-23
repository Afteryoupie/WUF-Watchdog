// SPDX-License-Identifier: Apache-2.0
package main

import (
	"syscall"
	"unsafe"
)

func showError(message string) {
	title, _ := syscall.UTF16PtrFromString("WUF Translate")
	body, _ := syscall.UTF16PtrFromString(message)
	syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)), 0x10)
}
