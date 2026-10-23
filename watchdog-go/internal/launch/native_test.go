// SPDX-License-Identifier: Apache-2.0
package launch

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBundlePathsResolveAfterMoving(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "launch.json")
	data, _ := json.Marshal(Config{Core: "watchdog.exe", Tray: "tray.exe", Arguments: []string{"--dashboard=resources/dashboard.html", "--laya-root", "resources/laya"}, TrayArguments: []string{"--config", "launch.json", "--assets", "resources/icons"}})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Core != filepath.Join(root, "watchdog.exe") || c.Arguments[0] != "--dashboard="+filepath.Join(root, "resources", "dashboard.html") || c.Arguments[2] != filepath.Join(root, "resources", "laya") || c.TrayArguments[1] != path {
		t.Fatalf("bundle paths were not resolved: %+v", c)
	}
}

func TestFramingRejectsTruncatedOversizeAndInvalidUTF8(t *testing.T) {
	for _, frame := range [][]byte{{1, 0}, {4, 0, 0, 0, '{'}, {0, 0, 0, 0}, {1, 0, 1, 0}, {1, 0, 0, 0, 0xff}} {
		if _, err := ReadMessage(bytes.NewReader(frame)); err == nil {
			t.Fatalf("invalid frame accepted: %v", frame)
		}
	}
	var buffer bytes.Buffer
	if err := WriteMessage(&buffer, map[string]string{"action": "start"}); err != nil {
		t.Fatal(err)
	}
	message, err := ReadMessage(&buffer)
	if err != nil {
		t.Fatal(err)
	}
	if string(message) != `{"action":"start"}` {
		t.Fatal(string(message))
	}
	if _, err := ReadMessage(&buffer); err != io.EOF {
		t.Fatal("clean browser EOF", err)
	}
	frame := make([]byte, 4)
	binary.LittleEndian.PutUint32(frame, 65537)
	if _, err := ReadMessage(bytes.NewReader(frame)); err == nil {
		t.Fatal("oversize frame accepted")
	}
}

func TestNativeServiceHelper(t *testing.T) {
	if os.Getenv("WUF_NATIVE_HELPER") != "1" {
		return
	}
	listener, err := net.Listen("tcp", os.Getenv("WUF_NATIVE_ADDRESS"))
	if err != nil {
		os.Exit(8)
	}
	stop := make(chan struct{}, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/health" {
			fmt.Fprintf(w, `{"status":"ok","service":"wuf-translate","protocol_version":1,"pid":%d,"llm_status":"sleeping"}`, os.Getpid())
		} else {
			io.WriteString(w, `{"status":"stopped"}`)
			select {
			case stop <- struct{}{}:
			default:
			}
		}
	})}
	go server.Serve(listener)
	select {
	case <-stop:
	case <-time.After(20 * time.Second):
	}
	server.Close()
	os.Exit(0)
}

func TestConcurrentColdStartAndServiceReadiness(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	t.Setenv("WUF_NATIVE_HELPER", "1")
	t.Setenv("WUF_NATIVE_ADDRESS", address)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := Config{Core: executable, Arguments: []string{"-test.run=^TestNativeServiceHelper$", "--"}, BaseURL: "http://" + address, DataDir: t.TempDir()}
	t.Cleanup(func() {
		request, err := http.NewRequest("POST", c.BaseURL+"/api/stop", nil)
		if err == nil {
			client := http.Client{Timeout: time.Second}
			if response, err := client.Do(request); err == nil {
				response.Body.Close()
			}
		}
	})
	var wg sync.WaitGroup
	replies := make(chan Reply, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			replies <- Start(ctx, c)
		}()
	}
	wg.Wait()
	close(replies)
	newInstances, pid := 0, 0
	for reply := range replies {
		if !reply.OK || !reply.ServiceReady || reply.ModelReady {
			t.Fatal("service readiness was confused with model readiness", reply)
		}
		if !reply.Already {
			newInstances++
		}
		if pid == 0 {
			pid = reply.PID
		} else if pid != reply.PID {
			t.Fatal("duplicate services were launched")
		}
	}
	if newInstances != 1 {
		t.Fatal("startup was not coordinated", newInstances)
	}
}

func TestWrongServiceIsNeverAdopted(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"status":"ok","pid":123,"service":"another-tool","protocol_version":1}`)
	}))
	defer upstream.Close()
	c := Config{Core: filepath.Join(t.TempDir(), "must-not-launch.exe"), BaseURL: upstream.URL, DataDir: t.TempDir()}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	reply := Start(ctx, c)
	if reply.OK || reply.ServiceReady || reply.Error != ErrWrongService.Error() {
		t.Fatal(reply)
	}
}

func TestConfigRejectsRemoteAndShellLaunchers(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "launch.json")
	for _, value := range []string{`{"core":"launch.bat","base_url":"http://127.0.0.1:8080"}`, `{"core":"watchdog.exe","base_url":"http://example.com:8080"}`, `{"core":"watchdog.exe","base_url":"http://127.0.0.1:8080/?token=secret"}`} {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(path); err == nil {
			t.Fatal("invalid launch config accepted", value)
		}
	}
	if err := os.WriteFile(path, []byte(`{"core":"watchdog.exe","base_url":"http://127.0.0.1:8080/"}`), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Core != filepath.Join(root, "watchdog.exe") || strings.HasSuffix(c.BaseURL, "/") {
		t.Fatal(c)
	}
}
