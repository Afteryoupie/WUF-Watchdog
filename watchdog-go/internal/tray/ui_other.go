//go:build !windows && !darwin

// SPDX-License-Identifier: Apache-2.0
package tray

import (
	"context"
	"errors"
)

func Run(ctx context.Context, client *Client, assets, swift string, ownerPID int, showPanel bool) error {
	return errors.New("native tray UI is supported on Windows and macOS")
}
