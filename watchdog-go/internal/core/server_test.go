// SPDX-License-Identifier: Apache-2.0
package core

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func testAPI(t *testing.T, m *Manager) (*API, string) {
	t.Helper()
	a := NewAPI(m, `<html><script>const s=__INITIAL_STATE_JSON__;</script>127.0.0.1:__PUBLIC_PORT__/v1</html>`, 8080)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := HTTPServer(a)
	finished := make(chan error, 1)
	go func() { finished <- Serve(server, listener) }()
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Error(err)
		}
		server.Close()
		if err := <-finished; err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Error(err)
		}
	})
	return a, "http://" + listener.Addr().String()
}

func readJSON(t *testing.T, method, url, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, payload
}

func rawPost(t *testing.T, address, body string, halfClose bool) (*net.TCPConn, *http.Response) {
	t.Helper()
	conn, err := net.Dial("tcp", strings.TrimPrefix(address, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	tcp := conn.(*net.TCPConn)
	t.Cleanup(func() { tcp.Close() })
	tcp.SetDeadline(time.Now().Add(6 * time.Second))
	fmt.Fprintf(tcp, "POST /v1/chat/completions HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
	if halfClose {
		if err := tcp.CloseWrite(); err != nil {
			t.Fatal(err)
		}
	}
	response, err := http.ReadResponse(bufio.NewReader(tcp), &http.Request{Method: "POST"})
	if err != nil {
		t.Fatal(err)
	}
	return tcp, response
}

func waitInactive(t *testing.T, m *Manager) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for m.Snapshot().ActiveRequests != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if m.Snapshot().ActiveRequests != 0 {
		t.Fatal("active request leaked")
	}
}

func TestHTTPStreamingHalfCloseQueuesNextModel(t *testing.T) {
	release := make(chan struct{})
	var count atomic.Int32
	captured := make(chan string, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		captured <- body["model"].(string)
		if count.Add(1) == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "data: first\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-release:
				io.WriteString(w, "data: [DONE]\n\n")
			case <-r.Context().Done():
				t.Error("half-closed client's stream was cancelled")
			}
		} else {
			io.WriteString(w, `{"ok":true}`)
		}
	}))
	t.Cleanup(upstream.Close)
	m := testManager(t)
	m.cfg.InternalPort = upstream.Listener.Addr().(*net.TCPAddr).Port
	writeFile(t, filepath.Join(m.cfg.ModelsDir, "other.gguf"))
	_, address := testAPI(t, m)
	_, response := rawPost(t, address, `{"model":"default","stream":true}`, true)
	defer response.Body.Close()
	lines := bufio.NewReader(response.Body)
	first, err := lines.ReadString('\n')
	if err != nil || first != "data: first\n" {
		t.Fatalf("first chunk %q: %v", first, err)
	}
	<-captured
	second := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 6 * time.Second}
		r, err := client.Post(address+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"other"}`))
		if err == nil {
			defer r.Body.Close()
			_, err = io.ReadAll(r.Body)
			if r.StatusCode != 200 {
				err = fmt.Errorf("HTTP %d", r.StatusCode)
			}
		}
		second <- err
	}()
	select {
	case value := <-captured:
		t.Fatal("second model started before stream completed", value)
	case <-time.After(30 * time.Millisecond):
	}
	if err := m.Sleep(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatal("stream did not hold request gate", err)
	}
	close(release)
	rest, err := io.ReadAll(lines)
	if err != nil || !strings.Contains(string(rest), "[DONE]") {
		t.Fatalf("truncated stream %q: %v", rest, err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if model := <-captured; model != filepath.Join(m.cfg.ModelsDir, "other.gguf") {
		t.Fatal("wrong second model", model)
	}
	waitInactive(t, m)
}

func TestHTTPResetCancelsUpstreamAndReleasesGate(t *testing.T) {
	cancelled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
			close(cancelled)
		case <-time.After(6 * time.Second):
			t.Error("upstream did not observe cancellation")
		}
	}))
	t.Cleanup(upstream.Close)
	m := testManager(t)
	m.cfg.InternalPort = upstream.Listener.Addr().(*net.TCPAddr).Port
	_, address := testAPI(t, m)
	conn, response := rawPost(t, address, `{"model":"default","stream":true}`, false)
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil || line != "data: first\n" {
		t.Fatal(line, err)
	}
	conn.SetLinger(0)
	conn.Close()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream not cancelled after TCP reset")
	}
	waitInactive(t, m)
	if err := m.Wake(context.Background()); err != nil {
		t.Fatal("request gate leaked", err)
	}
}

func TestHTTPPayloadAdaptationAndErrors(t *testing.T) {
	captured := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		captured <- payload
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(429)
		io.WriteString(w, `{"error":{"message":"rate limited"}}`)
	}))
	t.Cleanup(upstream.Close)
	m := testManager(t)
	m.cfg.InternalPort = upstream.Listener.Addr().(*net.TCPAddr).Port
	writeFile(t, filepath.Join(m.cfg.ModelsDir, "Riva-Translate-test.gguf"))
	_, address := testAPI(t, m)
	code, payload := readJSON(t, "POST", address+"/v1/chat/completions", `{"model":"Riva-Translate-test","wuf_target_language":"Traditional Chinese","messages":[{"role":"user","content":"Hello"}]}`)
	if code != 429 || payload["error"].(map[string]any)["message"] != "rate limited" {
		t.Fatal(code, payload)
	}
	request := <-captured
	if _, exists := request["wuf_target_language"]; exists {
		t.Fatal("metadata leaked upstream")
	}
	message := request["messages"].([]any)[0].(map[string]any)["content"]
	if message != "Translate this into Traditional Chinese: Hello" {
		t.Fatal(message)
	}
	for _, body := range []string{`{"model":"unknown"}`, `{"model":123}`, `[]`, `not-json`} {
		code, _ := readJSON(t, "POST", address+"/v1/chat/completions", body)
		if code != 400 {
			t.Fatal("invalid model/payload", code, body)
		}
	}
}

func TestSetupEndpoint(t *testing.T) {
	m := testManager(t)
	_, address := testAPI(t, m)
	code, body := readJSON(t, "GET", address+"/api/setup", "")
	if code != 200 || body["platform"] == "" {
		t.Fatalf("GET /api/setup = (%d, %v)", code, body)
	}
	code, _ = readJSON(t, "GET", address+"/api/setup?action=enable-autostart", "")
	if code != 405 {
		t.Fatalf("GET /api/setup?action=enable-autostart = %d, want 405", code)
	}
	code, body = readJSON(t, "POST", address+"/api/setup", `{"action":"unknown"}`)
	if code != 400 || body["status"] != "error" {
		t.Fatalf("POST /api/setup unknown = (%d, %v)", code, body)
	}
}

type failingWriteConn struct {
	net.Conn
	fail *atomic.Bool
}

func (c *failingWriteConn) Write(p []byte) (int, error) {
	if c.fail.CompareAndSwap(true, false) {
		return 0, syscall.EPIPE
	}
	return c.Conn.Write(p)
}

func TestHTTPReverseProxyRetriesStaleKeepAliveOnPost(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("upstream decode failed: %v", err)
			w.WriteHeader(400)
			return
		}
		if payload["messages"] == nil {
			t.Errorf("missing messages in replayed request: %#v", payload)
		}
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(upstream.Close)

	m := testManager(t)
	m.cfg.InternalPort = upstream.Listener.Addr().(*net.TCPAddr).Port
	api, address := testAPI(t, m)

	var dials atomic.Int32
	var failNextWrite atomic.Bool
	baseDial := api.transport.DialContext
	api.transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := baseDial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		if dials.Add(1) == 1 {
			return &failingWriteConn{Conn: conn, fail: &failNextWrite}, nil
		}
		return conn, nil
	}

	reqBody := `{"model":"default","messages":[{"role":"user","content":"Hello"}]}`
	code, resp := readJSON(t, "POST", address+"/v1/chat/completions", reqBody)
	if code != 200 || resp["ok"] != true {
		t.Fatalf("first request = (%d, %v)", code, resp)
	}

	// Simulate the upstream closing an idle keep-alive socket just as the second POST writes.
	failNextWrite.Store(true)
	code, resp = readJSON(t, "POST", address+"/v1/chat/completions", reqBody)
	if code != 200 || resp["ok"] != true {
		t.Fatalf("second request after EPIPE on pooled conn = (%d, %v)", code, resp)
	}
	if dials.Load() != 2 {
		t.Fatalf("expected second request to dial a fresh connection; dials=%d", dials.Load())
	}
	if upstreamCalls.Load() != 2 {
		t.Fatalf("expected upstream to handle 2 requests; calls=%d", upstreamCalls.Load())
	}
}
