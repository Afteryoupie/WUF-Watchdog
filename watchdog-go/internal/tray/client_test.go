// SPDX-License-Identifier: Apache-2.0
package tray

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestPresentationPreservesModelIdentityAndActivity(t *testing.T) {
	a := Compact("Very-long-translation-model-name-Instruct-Q4_K_M")
	b := Compact("Very-long-translation-model-name-Instruct-Q8_0")
	if a == b {
		t.Fatal("compact labels hid quantization identity")
	}
	state := &Snapshot{Status: "running", IdleTimeout: 180, IdleRemaining: 65, AutoSleepEnabled: true}
	if IconState(state) != "ready" || Countdown(state, 2*time.Second) != "01:03" {
		t.Fatal("ready presentation")
	}
	state.ActiveRequests = 1
	if IconState(state) != "running" || Countdown(state, time.Second) != "處理中…" {
		t.Fatal("active presentation")
	}
	state.Status = "sleeping"
	state.ActiveRequests = 0
	if IconState(state) != "sleeping" {
		t.Fatal("sleeping presentation")
	}
	if IconState(nil) != "offline" {
		t.Fatal("offline presentation")
	}
}

func TestActionSetupPostsJSONBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/setup" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected Content-Type: %q", r.Header.Get("Content-Type"))
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["action"] != "enable-autostart" {
			t.Fatalf("unexpected body: %+v", payload)
		}
		io.WriteString(w, `{"status":"ok"}`)
	}))
	defer server.Close()
	if err := NewClient(server.URL).Action(context.Background(), "setup", url.Values{"action": {"enable-autostart"}}); err != nil {
		t.Fatal(err)
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
