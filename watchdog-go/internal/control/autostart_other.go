//go:build !windows

// SPDX-License-Identifier: Apache-2.0
package control

import "context"

func platformAutostart(context.Context, string, *bool) (AutostartStatus, error) {
	return AutostartStatus{}, nil
}
