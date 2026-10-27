// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Keep the native contract fixture inside this module. Legacy worktrees may
// carry Python mock-specific messages and unavailable-model identities.
func TestPortableHTTPContract(t *testing.T) {
	data, err := os.ReadFile("testdata/watchdog-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Model    Model          `json:"model"`
		Decision map[string]any `json:"decision"`
		Cases    []struct {
			Name, Method, Path string
			Code               int
			JSON               map[string]any    `json:"json"`
			Body               json.RawMessage   `json:"body"`
			Headers            map[string]string `json:"headers"`
			ResponseHeaders    map[string]string `json:"response_headers"`
			Contains           string            `json:"contains"`
			Busy               bool              `json:"busy"`
			DecisionBusy       bool              `json:"decision_busy"`
			WakeOK             *bool             `json:"wake_ok"`
			Setup              struct {
				Starting bool `json:"is_starting"`
			} `json:"setup"`
			Calls      []string `json:"calls"`
			ShouldExit bool     `json:"should_exit"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, testCase := range fixture.Cases {
		t.Run(testCase.Name, func(t *testing.T) {
			c := testConfig(t)
			c.Binaries.FM, c.Binaries.AppleAPI = "", ""
			c.ModelsDir = t.TempDir()
			path := writeFile(t, filepath.Join(c.ModelsDir, "fixture-translation.gguf"))
			m, err := NewManager(c)
			if err != nil {
				t.Fatal(err)
			}
			var launched atomic.Int32
			m.start = func(context.Context, Model) (*child, error) {
				launched.Add(1)
				if testCase.WakeOK != nil && !*testCase.WakeOK {
					return nil, errors.New("fixture load failure")
				}
				return helper(t, false), nil
			}
			m.probe = func(context.Context, Model) error { return nil }
			m.starting = testCase.Setup.Starting
			a, address := testAPI(t, m, nil)
			d := a.decision
			for _, call := range testCase.Calls {
				if call == "decision_wake" {
					d.manager.current = Model{ID: DecisionModelID, Name: "Laya multilingual", Path: "/fixture/laya", Type: "laya", IdleTimeout: 180}
					d.manager.start = func(context.Context, Model) (*child, error) { return helper(t, false), nil }
					d.manager.probe = func(context.Context, Model) error { return nil }
				}
			}
			if testCase.Busy {
				if err := m.acquire(context.Background(), true); err != nil {
					t.Fatal(err)
				}
				defer m.release()
			}
			if testCase.DecisionBusy {
				if err := d.manager.acquire(context.Background(), true); err != nil {
					t.Fatal(err)
				}
				defer d.manager.release()
			}
			req, err := http.NewRequest(testCase.Method, address+testCase.Path, strings.NewReader(string(testCase.Body)))
			if err != nil {
				t.Fatal(err)
			}
			for key, value := range testCase.Headers {
				req.Header.Set(key, value)
			}
			if len(testCase.Body) > 0 {
				req.Header.Set("Content-Type", "application/json")
			}
			client := &http.Client{Timeout: 6 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			response, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			content, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != testCase.Code {
				t.Fatalf("HTTP %d, expected %d: %s", response.StatusCode, testCase.Code, content)
			}
			for key, value := range testCase.ResponseHeaders {
				if response.Header.Get(key) != value {
					t.Fatal(key, response.Header.Get(key))
				}
			}
			if testCase.JSON != nil {
				var actual map[string]any
				if err := json.Unmarshal(content, &actual); err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(testCase.JSON)
				if err != nil {
					t.Fatal(err)
				}
				quotedPath, _ := json.Marshal(path)
				expectedJSON := strings.ReplaceAll(string(encoded), `"/fixture/models/translation.gguf"`, string(quotedPath))
				expectedJSON = strings.ReplaceAll(expectedJSON, "Fixture Translation", "fixture-translation")
				var expected map[string]any
				if err := json.Unmarshal([]byte(expectedJSON), &expected); err != nil {
					t.Fatal(err)
				}
				for key, value := range expected {
					if !reflect.DeepEqual(actual[key], value) {
						t.Errorf("%s: %#v != %#v", key, actual[key], value)
					}
				}
				if testCase.Path == "/api/status" {
					if !reflect.DeepEqual(actual["decision"], fixture.Decision) {
						t.Errorf("decision shape: %#v", actual["decision"])
					}
				}
				if response.Header.Get("Access-Control-Allow-Origin") != "*" {
					t.Fatal("missing CORS")
				}
			}
			if testCase.Contains != "" && !strings.Contains(string(content), testCase.Contains) {
				t.Fatal("missing response text")
			}
			wantsLaunch := false
			for _, call := range testCase.Calls {
				wantsLaunch = wantsLaunch || call == "start" || call == "switch"
			}
			if wantsLaunch && launched.Load() != 1 {
				t.Fatal("expected one launch", launched.Load())
			}
			if !wantsLaunch && launched.Load() != 0 {
				t.Fatal("passive route launched a model")
			}
			if testCase.ShouldExit {
				select {
				case <-a.Done():
				case <-time.After(3 * time.Second):
					t.Fatal("stop did not complete")
				}
			}
		})
	}
}
