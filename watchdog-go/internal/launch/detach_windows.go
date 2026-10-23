// SPDX-License-Identifier: Apache-2.0
package launch

import (
	"os/exec"
	"syscall"
)

func detach(cmd *exec.Cmd, breakaway bool) {
	flags := uint32(0x08000000 | 0x00000008)
	if breakaway {
		flags |= 0x01000000
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: flags}
}
