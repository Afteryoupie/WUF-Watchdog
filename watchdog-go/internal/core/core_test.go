// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func writeFile(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testConfig(t *testing.T) Config {
	t.Helper()
	c, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	c.ModelsDir, c.DataDir = filepath.Join(root, "models"), filepath.Join(root, "data")
	c.StartupTimeout, c.QueueTimeout, c.ProbeInterval = 3*time.Second, 5*time.Second, time.Millisecond
	writeFile(t, filepath.Join(c.ModelsDir, "fixture.gguf"))
	return c
}

func TestDiscoveryAndPersistence(t *testing.T) {
	c := testConfig(t)
	c.Platform = "windows"
	writeFile(t, filepath.Join(c.ModelsDir, "Qwen3VL-test", "weights.gguf"))
	writeFile(t, filepath.Join(c.ModelsDir, "Qwen3VL-test", "mmproj-f16.gguf"))
	writeFile(t, filepath.Join(c.ModelsDir, "decision", "rl_agent_config.json"))
	writeFile(t, filepath.Join(c.ModelsDir, "decision", "model.gguf"))
	writeFile(t, filepath.Join(c.ModelsDir, "mlx", "config.json"))
	writeFile(t, filepath.Join(c.ModelsDir, "mmproj-only.gguf"))
	writeFile(t, filepath.Join(c.ModelsDir, ".hidden.gguf"))
	models, err := Discover(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "Qwen3VL-test" || !models[0].IsTranslation {
		t.Fatalf("models: %+v", models)
	}
	if Projector(c, models[0]) != filepath.Join(c.ModelsDir, "Qwen3VL-test", "mmproj-f16.gguf") {
		t.Fatal("nested projector missing")
	}
	for _, alias := range []string{models[0].ID, models[0].Name, models[0].Path} {
		if found, ok := FindModel(models, alias); !ok || found.ID != models[0].ID {
			t.Fatal(alias)
		}
	}
	for _, id := range []string{models[0].ID, models[1].ID} {
		if err := SaveLastModel(c.DataDir, id); err != nil {
			t.Fatal(err)
		}
		last, err := LoadLastModel(c.DataDir)
		if err != nil || last != id {
			t.Fatalf("last=%q err=%v", last, err)
		}
	}
	m, err := NewManager(c)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	if m.Snapshot().CurrentModelID != models[1].ID {
		t.Fatal("last model was not restored")
	}
	c.Platform = "darwin"
	c.Binaries.AppleAPI = writeFile(t, filepath.Join(t.TempDir(), "AppleAPI"))
	models, err = Discover(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 4 {
		t.Fatalf("mac models: %+v", models)
	}
	found, ok := FindModel(models, "Apple Foundation Models")
	if !ok || found.Type != "apple-api" {
		t.Fatalf("Apple: %+v", found)
	}
}

func TestCommandsUseCorePaths(t *testing.T) {
	c := testConfig(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c.Binaries = Binaries{Llama: executable, K2Llama: executable, Python: executable,
		OMLX: executable, FM: executable, AppleAPI: executable}
	c.Platform = "darwin"
	model := Model{ID: "fixture", Name: "Fixture", Path: filepath.Join(c.ModelsDir, "fixture.gguf"), Type: "gguf"}
	cases := []struct {
		kind, path string
		args       []string
		health     string
	}{
		{"gguf", model.Path, []string{"--model", model.Path, "--host", "127.0.0.1", "--port", "8081", "--ctx-size", "32768", "--n-gpu-layers", "99", "--batch-size", "512", "--ubatch-size", "512", "--flash-attn", "on", "--cache-type-k", "q8_0", "--cache-type-v", "q8_0"}, "/health"},
		{"apple-api", "permissive", []string{"serve", "--hostname", "127.0.0.1", "--port", "8081"}, "/api/v1/models"},
		{"apple", "system", []string{"serve", "--host", "127.0.0.1", "--port", "8081"}, "/v1/models"},
		{"omlx", model.Path, []string{"serve", "--model-dir", c.ModelsDir, "--host", "127.0.0.1", "--port", "8081", "--log-level", "warning"}, "/health"},
		{"mlx", model.Path, []string{"-m", "mlx_lm.server", "--model", model.Path, "--host", "127.0.0.1", "--port", "8081", "--max-tokens", "32768"}, "/health"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			m := model
			m.Type, m.Path = tc.kind, tc.path
			argv, err := Command(c, m)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(argv[1:], tc.args) {
				t.Fatalf("argv=%v", argv)
			}
			if HealthPath(m) != tc.health {
				t.Fatal("wrong health route")
			}
		})
	}
	model.ID = "K2-Horizon"
	argv, err := Command(c, model)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(argv[1:], " ") != "-m "+model.Path+" --host 127.0.0.1 --port 8081 -c 16384 -np 1 -ngl 99 --jinja --no-webui" {
		t.Fatal(argv)
	}
	c.Binaries.K2Llama = ""
	if _, err := Command(c, model); err == nil {
		t.Fatal("K2 must not fall back to stock llama")
	}
	model.ID = "fixture"
	for _, shell := range []string{"launch.bat", "launch.command", "launch.cmd", "launch.ps1"} {
		c.Binaries.Llama = writeFile(t, filepath.Join(t.TempDir(), shell))
		if _, err := Command(c, model); err == nil {
			t.Fatal("accepted shell shortcut", shell)
		}
	}
}

// A controlled real subprocess proves that timeout/stop reap owned processes.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("WUF_GO_HELPER") != "1" {
		return
	}
	if os.Getenv("WUF_GO_HELPER_EXIT") == "1" {
		os.Exit(7)
	}
	for {
		time.Sleep(time.Second)
	}
}

func helper(t *testing.T, exit bool) *child {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(), "WUF_GO_HELPER=1", fmt.Sprintf("WUF_GO_HELPER_EXIT=%d", map[bool]int{false: 0, true: 1}[exit]))
	configureProcess(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := watchProcess(cmd, func() {})
	t.Cleanup(func() {
		if err := p.stop(); err != nil {
			t.Error(err)
		}
	})
	return p
}

func testManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Stop(); err != nil {
			t.Error(err)
		}
	})
	m.start = func(context.Context, Model) (*child, error) { return helper(t, false), nil }
	m.probe = func(context.Context, Model) error { return nil }
	return m
}

func TestConcurrentWakeAndPassiveStatus(t *testing.T) {
	m := testManager(t)
	var launches atomic.Int32
	m.start = func(context.Context, Model) (*child, error) { launches.Add(1); return helper(t, false), nil }
	if m.Snapshot().Status != "sleeping" || launches.Load() != 0 {
		t.Fatal("status must remain passive")
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := m.Wake(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if launches.Load() != 1 || m.Snapshot().Status != "running" {
		t.Fatal("startup was not shared")
	}
	if err := m.Sleep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.Snapshot().Status != "sleeping" {
		t.Fatal("sleep did not clear readiness")
	}
}

func TestReadinessTimeoutAndEarlyExit(t *testing.T) {
	for _, early := range []bool{false, true} {
		t.Run(strconv.FormatBool(early), func(t *testing.T) {
			m := testManager(t)
			m.cfg.StartupTimeout = 150 * time.Millisecond
			var owned *child
			m.start = func(context.Context, Model) (*child, error) { owned = helper(t, early); return owned, nil }
			m.probe = func(context.Context, Model) error { return errors.New("not ready") }
			if err := m.Wake(context.Background()); err == nil {
				t.Fatal("unready process accepted")
			}
			select {
			case <-owned.done:
			default:
				t.Fatal("unready process leaked")
			}
			if m.Snapshot().Status != "sleeping" {
				t.Fatal("unready process reported running")
			}
		})
	}
}

func TestRequestGateCancellationSwitchAndIdle(t *testing.T) {
	m := testManager(t)
	started, finish := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- m.WithModel(context.Background(), "default", func(ctx context.Context, model Model) error {
			close(started)
			select {
			case <-finish:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	if m.Snapshot().ActiveRequests != 1 {
		t.Fatal("active request not tracked")
	}
	if err := m.Switch(context.Background(), "fixture"); !errors.Is(err, ErrBusy) {
		t.Fatal("switch interrupted request", err)
	}
	if err := m.Sleep(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatal("sleep interrupted request", err)
	}
	if err := m.CheckIdle(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := m.Wake(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("queued cancellation", err)
	}
	close(finish)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if m.Snapshot().ActiveRequests != 0 || m.Snapshot().RequestCount != 1 {
		t.Fatal("completion not tracked")
	}
	m.mu.Lock()
	m.lastActive = time.Now().Add(-200 * time.Second)
	m.mu.Unlock()
	off := false
	m.SetAutoSleep(&off)
	if err := m.CheckIdle(); err != nil {
		t.Fatal(err)
	}
	if m.Snapshot().Status != "running" {
		t.Fatal("disabled idle sleep was ignored")
	}
	m.SetAutoSleep(nil)
	if err := m.CheckIdle(); err != nil {
		t.Fatal(err)
	}
	if m.Snapshot().Status != "sleeping" {
		t.Fatal("idle did not stop model")
	}
}

func TestStopCancelsActiveAndQueuedRequests(t *testing.T) {
	m := testManager(t)
	started := make(chan struct{})
	active := make(chan error, 1)
	go func() {
		active <- m.WithModel(context.Background(), "", func(ctx context.Context, model Model) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	queued := make(chan error, 1)
	go func() { queued <- m.Wake(context.Background()) }()
	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-active:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("active request not cancelled")
	}
	select {
	case err := <-queued:
		if !errors.Is(err, ErrStopped) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queued request not cancelled")
	}
	if m.Snapshot().ActiveRequests != 0 {
		t.Fatal("active count leaked")
	}
	if err := m.Wake(context.Background()); !errors.Is(err, ErrStopped) {
		t.Fatal("service resurrected", err)
	}
}

func TestStopDuringStartupAndRetryAfterLaunchFailure(t *testing.T) {
	m := testManager(t)
	m.start = func(context.Context, Model) (*child, error) { return nil, errors.New("controlled launch failure") }
	if err := m.Wake(context.Background()); err == nil {
		t.Fatal("launch failure ignored")
	}
	if m.Snapshot().Status != "sleeping" {
		t.Fatal("launch failure left starting state")
	}
	var owned *child
	probing := make(chan struct{})
	m.start = func(context.Context, Model) (*child, error) { owned = helper(t, false); return owned, nil }
	m.probe = func(ctx context.Context, model Model) error {
		close(probing)
		<-ctx.Done()
		return ctx.Err()
	}
	result := make(chan error, 1)
	go func() { result <- m.Wake(context.Background()) }()
	select {
	case <-probing:
	case <-time.After(5 * time.Second):
		t.Fatal("startup did not probe")
	}
	if m.Snapshot().Status != "starting" {
		t.Fatal("unready child was reported ready")
	}
	if err := m.Sleep(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatal("startup was interrupted by sleep", err)
	}
	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancelled startup succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("startup did not cancel")
	}
	select {
	case <-owned.done:
	default:
		t.Fatal("startup child was not reaped")
	}
	if m.Snapshot().Status != "sleeping" {
		t.Fatal("cancelled startup left stale state")
	}
}

func TestQueuedRequestSwitchesOnlyAfterCompletion(t *testing.T) {
	m := testManager(t)
	m.mu.Lock()
	other := m.current
	other.ID, other.Name, other.Path = "other", "Other", "/fixture/other.gguf"
	m.models = append(m.models, other)
	m.mu.Unlock()
	started, finish := make(chan struct{}), make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- m.WithModel(context.Background(), "default", func(ctx context.Context, model Model) error {
			close(started)
			select {
			case <-finish:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first request did not start")
	}
	// Queue a different model while the first response is held open.
	queued := make(chan struct{})
	second := make(chan error, 1)
	go func() {
		close(queued)
		second <- m.WithModel(context.Background(), other.Path, func(ctx context.Context, model Model) error {
			if model.ID != other.ID {
				return errors.New("wrong model routed")
			}
			return nil
		})
	}()
	<-queued
	select {
	case err := <-second:
		t.Fatalf("request switched during active response: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if m.Snapshot().CurrentModelID == other.ID {
		t.Fatal("in-flight model changed")
	}
	close(finish)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	last, err := LoadLastModel(m.cfg.DataDir)
	if err != nil || last != other.ID {
		t.Fatalf("last model %q: %v", last, err)
	}
	if m.Snapshot().RequestCount != 2 {
		t.Fatal("queued request was lost")
	}
}

func TestOccupiedBackendPortCannotBeAdopted(t *testing.T) {
	c := testConfig(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	c.InternalPort = listener.Addr().(*net.TCPAddr).Port
	c.Binaries.Llama, err = os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	model := Model{ID: "fixture", Name: "Fixture", Type: "gguf", Path: filepath.Join(c.ModelsDir, "fixture.gguf")}
	if _, err := startProcess(context.Background(), c, model); err == nil {
		t.Fatal("occupied port adopted instead of failing before launch")
	}
}

func TestHealthRequiresHTTP200AndCorrectPath(t *testing.T) {
	for _, kind := range []string{"gguf", "apple", "apple-api"} {
		t.Run(kind, func(t *testing.T) {
			var code atomic.Int32
			code.Store(503)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != HealthPath(Model{Type: kind}) {
					t.Error("wrong path", r.URL.Path)
				}
				w.WriteHeader(int(code.Load()))
			}))
			defer upstream.Close()
			c := testConfig(t)
			c.InternalPort = upstream.Listener.Addr().(*net.TCPAddr).Port
			m, err := NewManager(c)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Stop()
			model := Model{Type: kind}
			if err := m.probe(context.Background(), model); err == nil {
				t.Fatal("HTTP 503 accepted")
			}
			code.Store(200)
			if err := m.probe(context.Background(), model); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSnapshotMatchesPortableFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/watchdog-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Model Model `json:"model"`
		Cases []struct {
			JSON map[string]any `json:"json"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	m := testManager(t)
	m.mu.Lock()
	m.current, m.models = fixture.Model, []Model{fixture.Model}
	m.mu.Unlock()
	encoded, err := json.Marshal(m.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	var actual map[string]any
	if err := json.Unmarshal(encoded, &actual); err != nil {
		t.Fatal(err)
	}
	delete(actual, "models")
	if !reflect.DeepEqual(actual, fixture.Cases[0].JSON) {
		t.Fatalf("Go status differs from the portable Python contract: %#v", actual)
	}
}
