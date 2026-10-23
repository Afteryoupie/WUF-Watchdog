// SPDX-License-Identifier: Apache-2.0
package tray

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

func Run(ctx context.Context, client *Client, assets, swift string, ownerPID int, showPanel bool) error {
	// The retained Swift client currently uses the established public 8080 API.
	if client.BaseURL != "http://127.0.0.1:8080" {
		return errors.New("retained Swift UI requires the standard loopback API on 8080")
	}
	if !filepath.IsAbs(swift) {
		return errors.New("Swift UI executable must be an absolute bundle path")
	}
	info, err := os.Stat(swift)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("Swift UI path is a directory")
	}
	command := exec.Command(swift)
	command.Stdout, command.Stderr = os.Stderr, os.Stderr
	if err := command.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- command.Wait(); close(exited) }()
	defer func() {
		command.Process.Kill()
		select {
		case <-exited:
		case <-time.After(2 * time.Second):
		}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case err := <-exited:
			return err
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if ownerPID > 0 {
				if err := syscall.Kill(ownerPID, 0); errors.Is(err, syscall.ESRCH) {
					return nil
				}
			}
		}
	}
}
