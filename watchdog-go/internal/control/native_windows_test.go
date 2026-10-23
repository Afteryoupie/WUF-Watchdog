// SPDX-License-Identifier: Apache-2.0
package control

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"wuf-translate/watchdog-go/internal/launch"
)

func testRunKey(t *testing.T) string {
	t.Helper()
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		t.Fatal(err)
	}
	guid := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	path := `Software\WUFTranslate\InstallTests\` + guid + `\Run`
	t.Cleanup(func() {
		parent := strings.TrimSuffix(path, `\Run`)
		wide, _ := syscall.UTF16PtrFromString(parent)
		result, _, _ := registry.NewProc("RegDeleteTreeW").Call(uintptr(syscall.HKEY_CURRENT_USER), uintptr(unsafe.Pointer(wide)))
		if result != 0 && result != 2 {
			t.Errorf("test key cleanup: %v", syscall.Errno(result))
		}
		registry.NewProc("RegDeleteKeyW").Call(uintptr(syscall.HKEY_CURRENT_USER), uintptr(unsafe.Pointer(wide)))
	})
	return path
}

func TestNativeAutostartEnableDisableForeignAndLegacy(t *testing.T) {
	path := testRunKey(t)
	command := `"C:\WUF package\app.exe" --config "C:\WUF package\launch.json" --quiet`
	legacy := `"C:\WUF package\app.exe" --quiet`
	if err := setAutostart(path, command, legacy, true); err != nil {
		t.Fatal(err)
	}
	value, found, err := readAutostart(path)
	if err != nil || !found || value != command {
		t.Fatalf("%q %v %v", value, found, err)
	}
	if err := setAutostart(path, command, legacy, false); err != nil {
		t.Fatal(err)
	}
	_, found, err = readAutostart(path)
	if err != nil || found {
		t.Fatal("owned value not removed", err)
	}
	if err := setAutostart(path, legacy, "", true); err != nil {
		t.Fatal(err)
	}
	if err := setAutostart(path, command, legacy, false); err != nil {
		t.Fatal("previous installer command cannot be cancelled", err)
	}
	if err := setAutostart(path, "foreign", "", true); err != nil {
		t.Fatal(err)
	}
	for _, enable := range []bool{true, false} {
		if err := setAutostart(path, command, legacy, enable); err == nil {
			t.Fatal("foreign entry changed")
		}
	}
	value, found, err = readAutostart(path)
	if err != nil || !found || value != "foreign" {
		t.Fatal("foreign entry not preserved", value, err)
	}
	if err := setAutostart(`Software\OtherApplication`, command, legacy, true); err == nil {
		t.Fatal("arbitrary test registry path accepted")
	}
}

func TestStopRejectsAnotherExecutableBeforePosting(t *testing.T) {
	var stopCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/stop" {
			stopCalls.Add(1)
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "service": "wuf-translate", "protocol_version": 1, "pid": os.Getpid()})
	}))
	defer server.Close()
	err := Stop(context.Background(), launch.Config{Core: filepath.Join(t.TempDir(), "other.exe"), BaseURL: server.URL})
	if err == nil || stopCalls.Load() != 0 {
		t.Fatal("sent stop to an unowned installation", err)
	}
}
func TestStopAlreadyOffline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	if err := Stop(context.Background(), launch.Config{BaseURL: "http://" + address}); err != nil {
		t.Fatal(err)
	}
}

func TestStopWaitsForOwnedProcessExit(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestControlHelper$")
	command.Env = append(os.Environ(), "WUF_CONTROL_HELPER=1")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if command.ProcessState == nil {
			command.Process.Kill()
			command.Wait()
		}
	}()
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		t.Fatal("helper did not publish API")
	}
	if err := Stop(ctx, launch.Config{Core: executable, BaseURL: scanner.Text()}); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
}
func TestControlHelper(t *testing.T) {
	if os.Getenv("WUF_CONTROL_HELPER") != "1" {
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(2)
	}
	fmt.Println("http://" + listener.Addr().String())
	err = http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/stop" {
			fmt.Fprint(w, `{"status":"stopped"}`)
			w.(http.Flusher).Flush()
			go func() { time.Sleep(25 * time.Millisecond); os.Exit(0) }()
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "service": "wuf-translate", "protocol_version": 1, "pid": os.Getpid()})
	}))
	if !errors.Is(err, http.ErrServerClosed) {
		os.Exit(3)
	}
}
