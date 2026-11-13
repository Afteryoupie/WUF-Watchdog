// SPDX-License-Identifier: Apache-2.0
package tray

import (
	"testing"
	"time"
	"wuf-translate/watchdog-go/internal/core"
)

func TestPanelLiveCountdownAndBusyState(t *testing.T) {
	d := &Snapshot{Status: "running", CurrentModel: "Riva-Translate-4B-Instruct-v2-Q4_K_M", IdleTimeout: 180, IdleRemaining: 170, AutoSleepEnabled: true, Models: []core.Model{{ID: "riva"}}}
	v := panelState(d, 3*time.Second, false, "")
	if v.countdown != "02:47" || v.progress < .92 || v.progress > .93 || !v.primaryEnabled {
		t.Fatalf("%+v", v)
	}
	d.ActiveRequests = 1
	v = panelState(d, 0, false, "")
	if v.primaryEnabled || v.modelsEnabled || v.countdown != "處理中…" {
		t.Fatalf("busy model: %+v", v)
	}
	d.Status = "sleeping"
	d.ActiveRequests = 0
	v = panelState(d, 0, false, "")
	if v.primary != "啟動模型" || v.progress != 0 {
		t.Fatalf("sleeping state: %+v", v)
	}
	if offline := panelState(nil, 0, false, ""); offline.primaryEnabled {
		t.Fatal("offline controls enabled")
	}
}
