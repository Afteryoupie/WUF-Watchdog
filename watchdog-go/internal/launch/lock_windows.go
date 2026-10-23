// SPDX-License-Identifier: Apache-2.0
package launch

import (
	"context"
	"errors"
	"runtime"
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var createMutex = kernel32.NewProc("CreateMutexW")
var waitObject = kernel32.NewProc("WaitForSingleObject")
var releaseMutex = kernel32.NewProc("ReleaseMutex")
var closeHandle = kernel32.NewProc("CloseHandle")

func Acquire(ctx context.Context, name, dir string) (func(), error) {
	wide, err := syscall.UTF16PtrFromString("Local\\WUF." + name)
	if err != nil {
		return nil, err
	}
	handle, _, callErr := createMutex.Call(0, 0, uintptr(unsafe.Pointer(wide)))
	if handle == 0 {
		return nil, callErr
	}
	runtime.LockOSThread()
	for {
		result, _, err := waitObject.Call(handle, 50)
		if result == 0 || result == 0x80 {
			return func() { releaseMutex.Call(handle); closeHandle.Call(handle); runtime.UnlockOSThread() }, nil
		}
		if result != 0x102 {
			closeHandle.Call(handle)
			runtime.UnlockOSThread()
			return nil, errors.New("cannot acquire WUF instance mutex: " + err.Error())
		}
		if err := ctx.Err(); err != nil {
			closeHandle.Call(handle)
			runtime.UnlockOSThread()
			return nil, err
		}
	}
}
