// SPDX-License-Identifier: Apache-2.0
package tray

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"wuf-translate/watchdog-go/internal/core"
)

func TestPresentationPreservesModelIdentityAndActivity(t *testing.T) {
	a := Compact("Very-long-translation-model-name-Instruct-Q4_K_M")
	b := Compact("Very-long-translation-model-name-Instruct-Q8_0")
	if a == b {
		t.Fatal("compact labels hid quantization identity")
	}
	state := &Snapshot{Snapshot: core.Snapshot{Status: "running", IdleTimeout: 180, IdleRemaining: 65, AutoSleepEnabled: true}}
	if IconState(state) != "ready" || Countdown(state, 2*time.Second) != "01:03" {
		t.Fatal("ready presentation")
	}
	state.ActiveRequests = 1
	if IconState(state) != "running" || Countdown(state, time.Second) != "處理中…" {
		t.Fatal("active presentation")
	}
	state.Status = "sleeping"
	state.ActiveRequests = 0
	state.Decision.Status = "running"
	state.Decision.ActiveRequests = 1
	if IconState(state) != "running" {
		t.Fatal("independent decision activity was hidden")
	}
	if IconState(nil) != "offline" {
		t.Fatal("offline presentation")
	}
}

func TestActionRejectsHTTP200LoadFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/wake" {
			t.Error("wrong action")
		}
		io.WriteString(w, `{"status":"error","state":"failed"}`)
	}))
	defer server.Close()
	if err := NewClient(server.URL).Action(context.Background(), "wake", nil); err == nil {
		t.Fatal("HTTP 200 was mistaken for successful readiness")
	}
}
