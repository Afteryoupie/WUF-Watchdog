//go:build !windows

// SPDX-License-Identifier: Apache-2.0
package launch

import (
	"os/exec"
	"syscall"
)

func detach(cmd *exec.Cmd, breakaway bool) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
