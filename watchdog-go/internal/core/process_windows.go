// SPDX-License-Identifier: Apache-2.0
package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

func killTree(cmd *exec.Cmd) error {
	// Target only the process tree this service owns, never an image name.
	kill := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "taskkill.exe"),
		"/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid))
	configureProcess(kill)
	if err := kill.Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
