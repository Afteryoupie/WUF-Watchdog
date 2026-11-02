// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"net/http/httptest"
	"strings"
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
