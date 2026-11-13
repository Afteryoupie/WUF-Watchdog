// SPDX-License-Identifier: Apache-2.0
//go:build darwin

package setup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFindAppBundle(t *testing.T) {
	cases := []struct {
		exe  string
		want string
		ok   bool
	}{
		{"/Users/test/Applications/WUF Watchdog.app/Contents/MacOS/watchdog", "/Users/test/Applications/WUF Watchdog.app", true},
		{"/Users/test/Applications/WUF Watchdog.app/Contents/MacOS/app", "/Users/test/Applications/WUF Watchdog.app", true},
		{"/tmp/watchdog", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := findAppBundle(tc.exe)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("findAppBundle(%q) = (%q, %v), want (%q, %v)", tc.exe, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDarwinSetupLifecycle(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	app := filepath.Join(home, "Applications", "WUF Watchdog.app")
	macos := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"app", "native-host", "tray", "watchdog"} {
		if err := os.WriteFile(filepath.Join(macos, name), []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	exe := filepath.Join(macos, "watchdog")

	st := QueryStatus(context.Background(), exe, "")
	if st.NativeHost != "not_installed" || st.Autostart != "disabled" || st.FMStatus != "missing" {
		t.Fatalf("unexpected initial status: %+v", st)
	}

	if err := EnsureNativeHost(exe); err != nil {
		t.Fatalf("EnsureNativeHost failed: %v", err)
	}
	st = QueryStatus(context.Background(), exe, "")
	if st.NativeHost != "installed" {
		t.Fatalf("expected installed native host, got %+v", st)
	}

	if err := SetAutostart(exe, true); err != nil {
		t.Fatalf("SetAutostart(true) failed: %v", err)
	}
	st = QueryStatus(context.Background(), exe, "")
	if st.Autostart != "enabled" {
		t.Fatalf("expected enabled autostart, got %+v", st)
	}

	if err := SetAutostart(exe, false); err != nil {
		t.Fatalf("SetAutostart(false) failed: %v", err)
	}
	st = QueryStatus(context.Background(), exe, "")
	if st.Autostart != "disabled" {
		t.Fatalf("expected disabled autostart, got %+v", st)
	}

	if err := Uninstall(exe); err != nil {
		t.Fatalf("Uninstall failed: %v", err)
	}
	st = QueryStatus(context.Background(), exe, "")
	if st.NativeHost != "not_installed" {
		t.Fatalf("expected not_installed after uninstall, got %+v", st)
	}
}

func TestQueryFMClassification(t *testing.T) {
	tmp := t.TempDir()
	okScript := filepath.Join(tmp, "fm-ok")
	licScript := filepath.Join(tmp, "fm-lic")
	if err := os.WriteFile(okScript, []byte("#!/bin/sh\necho 'System model available'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(licScript, []byte("#!/bin/sh\necho 'Please accept license terms via sudo fm license' >&2\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if s, _ := queryFM(context.Background(), okScript); s != "available" {
		t.Fatalf("expected available, got %q", s)
	}
	if s, _ := queryFM(context.Background(), licScript); s != "license_required" {
		t.Fatalf("expected license_required, got %q", s)
	}
}
