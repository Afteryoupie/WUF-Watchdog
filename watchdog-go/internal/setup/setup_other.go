// SPDX-License-Identifier: Apache-2.0
//go:build !darwin && !windows

package setup

import (
	"context"
	"errors"
)

func ensureNativeHost(string) error { return nil }

func queryPlatformStatus(context.Context, string, string) Status {
	return Status{
		NativeHost: "unsupported",
		Autostart:  "unsupported",
		FMStatus:   "not_applicable",
	}
}

func setAutostart(string, bool) error {
	return errors.New("autostart is only supported on macOS and Windows")
}

func uninstall(string) error {
	return errors.New("uninstall is only supported on macOS and Windows")
}
