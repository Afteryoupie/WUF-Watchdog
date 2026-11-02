// SPDX-License-Identifier: Apache-2.0
package control

import (
	"context"
	"sync"
)

type AutostartStatus struct {
	Supported bool `json:"supported"`
	Enabled   bool `json:"enabled"`
}

var autostartMu sync.Mutex

// Autostart reads the OS setting, or applies an explicit desired value.
func Autostart(ctx context.Context, config string, enabled *bool) (AutostartStatus, error) {
	autostartMu.Lock()
	defer autostartMu.Unlock()
	return platformAutostart(ctx, config, enabled)
}
