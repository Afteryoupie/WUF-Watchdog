// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"wuf-translate/watchdog-go/internal/control"
)

func TestAutostartDashboardBoundary(t *testing.T) {
	for _, tt := range []struct {
		name, method, host, origin, contentType, body string
		code                                          int
		calls                                         int
	}{
		{"read", "GET", "127.0.0.1:8080", "", "", "", 200, 1},
		{"enable", "POST", "127.0.0.1:8080", "http://127.0.0.1:8080", "application/json", `{"enabled":true}`, 200, 1},
		{"disable", "POST", "127.0.0.1:8080", "", "application/json", `{"enabled":false}`, 200, 1},
		{"foreign origin", "POST", "127.0.0.1:8080", "https://example.com", "application/json", `{"enabled":true}`, 403, 0},
		{"rebound host", "POST", "example.com:8080", "http://example.com:8080", "application/json", `{"enabled":true}`, 403, 0},
		{"form", "POST", "127.0.0.1:8080", "", "text/plain", `{"enabled":true}`, 415, 0},
		{"string bool", "POST", "127.0.0.1:8080", "", "application/json", `{"enabled":"false"}`, 400, 0},
		{"missing value", "POST", "127.0.0.1:8080", "", "application/json", `{}`, 400, 0},
		{"preflight", "OPTIONS", "127.0.0.1:8080", "https://example.com", "", "", 405, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			a := &API{Autostart: func(_ context.Context, enabled *bool) (control.AutostartStatus, error) {
				calls++
				if tt.method == "GET" && enabled != nil {
					t.Fatal("GET must not mutate")
				}
				if tt.method == "POST" && (enabled == nil || *enabled != strings.Contains(tt.body, "true")) {
					t.Fatal("explicit desired state lost")
				}
				return control.AutostartStatus{Supported: true, Enabled: enabled != nil && *enabled}, nil
			}}
			r := httptest.NewRequest(tt.method, "http://"+tt.host+"/api/autostart", strings.NewReader(tt.body))
			r.Header.Set("Origin", tt.origin)
			r.Header.Set("Content-Type", tt.contentType)
			w := httptest.NewRecorder()
			a.ServeHTTP(w, r)
			if w.Code != tt.code || calls != tt.calls {
				t.Fatalf("code=%d calls=%d body=%s", w.Code, calls, w.Body.String())
			}
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("OS control must not allow cross origin access")
			}
		})
	}
}

func TestManagementEndpointsRejectCrossOriginAndPreserveValidCallers(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", tmpHome)

	m := testManager(t)
	var launched atomic.Int32
	m.start = func(context.Context, Model) (*child, error) {
		launched.Add(1)
		return helper(t, false), nil
	}
	a := NewAPI(m, `<html>dashboard</html>`, 8080)
	t.Cleanup(func() { _ = a.Close() })

	assertEmptyHome := func(t *testing.T, label string) {
		t.Helper()
		entries, err := os.ReadDir(tmpHome)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("%s caused filesystem side effect in %s: %+v", label, tmpHome, entries)
		}
	}

	setupRejects := []struct {
		name, method, url, origin, site, contentType, body string
		wantCode                                           int
	}{
		{"foreign origin", "POST", "http://127.0.0.1:8080/api/setup", "https://example.com", "", "application/json", `{"action":"install-native-host"}`, 403},
		{"cross-port origin", "POST", "http://127.0.0.1:8080/api/setup", "http://127.0.0.1:3000", "", "application/json", `{"action":"enable-autostart"}`, 403},
		{"extension rejected on setup", "POST", "http://127.0.0.1:8080/api/setup", "chrome-extension://abcdefghijklmnop", "cross-site", "application/json", `{"action":"install-native-host"}`, 403},
		{"cross-site fetch metadata", "POST", "http://127.0.0.1:8080/api/setup", "", "cross-site", "application/json", `{"action":"install-native-host"}`, 403},
		{"rebound host", "POST", "http://evil.example.com:8080/api/setup", "", "", "application/json", `{"action":"install-native-host"}`, 403},
		{"get with action", "GET", "http://127.0.0.1:8080/api/setup?action=install-native-host", "http://127.0.0.1:8080", "same-origin", "", "", 405},
		{"non-json post", "POST", "http://127.0.0.1:8080/api/setup?action=install-native-host", "http://127.0.0.1:8080", "same-origin", "text/plain", `{"action":"install-native-host"}`, 415},
		{"options preflight", "OPTIONS", "http://127.0.0.1:8080/api/setup", "https://example.com", "", "", "", 405},
	}
	for _, tc := range setupRejects {
		t.Run("setup_"+tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.url, strings.NewReader(tc.body))
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.site != "" {
				r.Header.Set("Sec-Fetch-Site", tc.site)
			}
			if tc.contentType != "" {
				r.Header.Set("Content-Type", tc.contentType)
			}
			w := httptest.NewRecorder()
			a.ServeHTTP(w, r)
			if w.Code != tc.wantCode {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.wantCode, w.Body.String())
			}
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatalf("unexpected CORS header: %q", w.Header().Get("Access-Control-Allow-Origin"))
			}
			assertEmptyHome(t, tc.name)
		})
	}

	for _, route := range []struct{ method, path string }{
		{"POST", "/api/wake"},
		{"POST", "/api/switch?model=fixture"},
		{"POST", "/api/sleep"},
		{"POST", "/api/toggle_auto_sleep"},
		{"POST", "/api/stop"},
		{"GET", "/stop"},
		{"GET", "/shutdown"},
	} {
		for _, bad := range []struct{ label, origin, site string }{
			{"foreign_origin", "https://example.com", ""},
			{"cross_site", "", "cross-site"},
		} {
			t.Run(route.path+"_"+bad.label, func(t *testing.T) {
				r := httptest.NewRequest(route.method, "http://127.0.0.1:8080"+route.path, nil)
				if bad.origin != "" {
					r.Header.Set("Origin", bad.origin)
				}
				if bad.site != "" {
					r.Header.Set("Sec-Fetch-Site", bad.site)
				}
				w := httptest.NewRecorder()
				a.ServeHTTP(w, r)
				if w.Code != 403 {
					t.Fatalf("%s %s (%s) = %d, want 403: %s", route.method, route.path, bad.label, w.Code, w.Body.String())
				}
				if w.Header().Get("Access-Control-Allow-Origin") != "" {
					t.Fatalf("unexpected CORS header on %s: %q", route.path, w.Header().Get("Access-Control-Allow-Origin"))
				}
				if launched.Load() != 0 {
					t.Fatalf("rejected request triggered launch on %s", route.path)
				}
				select {
				case <-a.Done():
					t.Fatalf("rejected stop request closed server")
				default:
				}
			})
		}
	}

	// Legitimate callers: dashboard same-origin, desktop client (empty Origin), and browser extension on /api/*
	t.Run("dashboard_same_origin", func(t *testing.T) {
		for _, req := range []struct {
			method, path, body string
			wantCode           int
		}{
			{"GET", "/api/status", "", 200},
			{"GET", "/api/setup", "", 200},
			{"POST", "/api/setup", `{"action":"unknown"}`, 400},
			{"POST", "/api/wake", "", 200},
		} {
			r := httptest.NewRequest(req.method, "http://127.0.0.1:8080"+req.path, strings.NewReader(req.body))
			r.Header.Set("Origin", "http://127.0.0.1:8080")
			r.Header.Set("Sec-Fetch-Site", "same-origin")
			if req.body != "" {
				r.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()
			a.ServeHTTP(w, r)
			if w.Code != req.wantCode {
				t.Fatalf("%s %s = %d, want %d: %s", req.method, req.path, w.Code, req.wantCode, w.Body.String())
			}
		}
	})

	t.Run("desktop_native_caller", func(t *testing.T) {
		for _, req := range []struct {
			method, path, body string
			wantCode           int
		}{
			{"GET", "/api/status", "", 200},
			{"POST", "/api/setup", `{"action":"unknown"}`, 400},
			{"POST", "/api/wake", "", 200},
		} {
			r := httptest.NewRequest(req.method, "http://127.0.0.1:8080"+req.path, strings.NewReader(req.body))
			if req.body != "" {
				r.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()
			a.ServeHTTP(w, r)
			if w.Code != req.wantCode {
				t.Fatalf("%s %s = %d, want %d: %s", req.method, req.path, w.Code, req.wantCode, w.Body.String())
			}
		}
	})

	t.Run("extension_caller", func(t *testing.T) {
		for _, req := range []struct {
			method, path string
		}{
			{"GET", "/api/status"},
			{"POST", "/api/wake"},
			{"POST", "/api/switch?model=fixture"},
		} {
			r := httptest.NewRequest(req.method, "http://127.0.0.1:8080"+req.path, nil)
			r.Header.Set("Origin", "chrome-extension://abcdefghijklmnop")
			r.Header.Set("Sec-Fetch-Site", "cross-site")
			w := httptest.NewRecorder()
			a.ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatalf("%s %s = %d, want 200: %s", req.method, req.path, w.Code, w.Body.String())
			}
		}
	})

	// Native desktop caller can stop server last.
	stopReq := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/stop", nil)
	stopW := httptest.NewRecorder()
	a.ServeHTTP(stopW, stopReq)
	if stopW.Code != 200 {
		t.Fatalf("desktop /api/stop = %d", stopW.Code)
	}
}
