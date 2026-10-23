//go:build !windows

// SPDX-License-Identifier: Apache-2.0
package control

import (
	"context"
	"errors"
)

func readAutostart(string) (string, bool, error) {
	return "", false, errors.New("Windows autostart requires Windows")
}
func setAutostart(string, string, string, bool) error {
	return errors.New("Windows autostart requires Windows")
}
func executablePath(int) (string, error) {
	return "", errors.New("this control command requires Windows")
}
func waitForExit(context.Context, int) error {
	return errors.New("this control command requires Windows")
}
