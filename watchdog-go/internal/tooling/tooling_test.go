// SPDX-License-Identifier: Apache-2.0
package tooling

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"wuf-translate/watchdog-go/internal/launch"
)

func put(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0755); err != nil {
		t.Fatal(err)
	}
}
func fixtureRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	for _, file := range []string{"watchdog/templates/dashboard.html", "watchdog/assets/guardian.icns", "watchdog-go/LICENSE", "watchdog/__init__.py", "watchdog/decision_protocol.py", "watchdog/laya_backend.py"} {
		put(t, filepath.Join(repo, filepath.FromSlash(file)), file)
	}
	return repo
}
func appFixture(t *testing.T) string {
	t.Helper()
	app := filepath.Join(t.TempDir(), "WUF & test.app")
	for _, name := range []string{"app", "native-host", "tray", "watchdog"} {
		put(t, filepath.Join(app, "Contents", "MacOS", name), "binary")
	}
	return app
}

func TestMacPackagePathsAndNoManagementPython(t *testing.T) {
	repo := fixtureRepo(t)
	app := filepath.Join(t.TempDir(), "WUF & example.app")
	if err := PopulateMac(repo, app); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(app, "Contents", "Info.plist"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := readPlist(data)
	if err != nil || info["CFBundleExecutable"] != "app" || info["LSUIElement"] != true {
		t.Fatalf("plist %v %v", info, err)
	}
	entries, err := os.ReadDir(filepath.Join(app, "Contents", "Resources", "laya", "watchdog"))
	if err != nil || len(entries) != 3 {
		t.Fatalf("Laya files %v %v", entries, err)
	}
	var config launch.Config
	raw, _ := os.ReadFile(filepath.Join(app, "Contents", "MacOS", "launch.json"))
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if config.Core != "watchdog" || config.Tray != "tray" {
		t.Fatal(config)
	}
	if _, err := os.Stat(filepath.Join(app, "Contents", "MacOS", "app")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("layout must not invent binaries")
	}
	if err := PopulateMac(repo, app); err == nil {
		t.Fatal("must refuse existing bundle")
	}
}

func TestMacInstallerForeignEntriesAndOwnership(t *testing.T) {
	app := appFixture(t)
	home := t.TempDir()
	if err := InstallMac("install", app, home, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := InstallMac("install", app, home, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	chrome := filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "NativeMessagingHosts", launch.HostName+".json")
	raw, _ := os.ReadFile(chrome)
	var host map[string]any
	_ = json.Unmarshal(raw, &host)
	origins := host["allowed_origins"].([]any)
	if len(origins) != 1 || origins[0] != launch.ExtensionOrigin {
		t.Fatal(host)
	}
	if err := InstallMac("enable-autostart", app, home, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	agent := filepath.Join(home, "Library", "LaunchAgents", LoginLabel+".plist")
	if err := InstallMac("disable-autostart", app, home, func() error { return errors.New("bootout failure") }); err == nil {
		t.Fatal("must report failed bootout")
	}
	if _, err := os.Stat(agent); err != nil {
		t.Fatal("must retain failed agent")
	}
	if err := InstallMac("disable-autostart", app, home, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	foreign := `{"name":"foreign","path":"other"}`
	put(t, chrome, foreign)
	if err := InstallMac("install", app, home, func() error { return nil }); err == nil {
		t.Fatal("foreign registration overwritten")
	}
	if err := InstallMac("uninstall", app, home, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(chrome)
	if string(after) != foreign {
		t.Fatal("foreign registration removed")
	}
}

func TestMacInstallerRollbackOnSecondParentFailure(t *testing.T) {
	app := appFixture(t)
	home := t.TempDir()
	put(t, filepath.Join(home, "Library", "Application Support", "Microsoft Edge"), "not a directory")
	if err := InstallMac("install", app, home, func() error { return nil }); err == nil {
		t.Fatal("expected parent conflict")
	}
	chrome := filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "NativeMessagingHosts", launch.HostName+".json")
	if _, err := os.Stat(chrome); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("partial registration remains")
	}
}

func TestSubtitleFormatsRollingAndRepeatedSpeech(t *testing.T) {
	vtt := "WEBVTT\n\nNOTE ignore this\ncaption note\n\n00:00:00.000 --> 00:00:02.000\n<c>Hello &amp; welcome</c>\n\n00:00:01.000 --> 00:00:03.000\nHello &amp; welcome back.\n\n00:00:05.000 --> 00:00:06.000\nHello &amp; welcome back.\n"
	entries, err := ParseSubtitles([]byte(vtt))
	if err != nil {
		t.Fatal(err)
	}
	texts := []string{}
	for _, entry := range entries {
		texts = append(texts, entry.Text)
	}
	if !reflect.DeepEqual(texts, []string{"Hello & welcome", "back.", "Hello & welcome back."}) {
		t.Fatal(texts)
	}
	if len(MergeSubtitles(entries)) != 2 {
		t.Fatal("sentence merge/repetition mismatch")
	}
	if _, err := ParseSubtitles([]byte("1\n00:00:02,000 --> 00:00:01,000\nhello\n")); err == nil {
		t.Fatal("negative interval accepted")
	}
	entries, err = ParseSubtitles([]byte(`{"events":[{"tStartMs":1000,"dDurationMs":500,"segs":[{"utf8":"Start "},{"utf8":"up."}]}]}`))
	if err != nil || len(entries) != 1 || entries[0].Text != "Start up." || entries[0].EndMS != 1500 {
		t.Fatalf("%v %v", entries, err)
	}
}

func TestSubtitleDiagnosticWireAndEvidence(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Error(r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["wuf_target_language"] != "Traditional Chinese" {
			t.Error(body)
		}
		if _, ok := body["model"]; ok {
			t.Error("server default model must be omitted")
		}
		requests++
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "啟動"}, "finish_reason": "stop"}}})
	}))
	defer server.Close()
	input := filepath.Join(t.TempDir(), "captions.srt")
	put(t, input, "1\n00:00:01,000 --> 00:00:03,000\nStart up.\n")
	output := filepath.Join(t.TempDir(), "evidence")
	err := RunSubtitleDiagnostic(context.Background(), SubtitleOptions{Input: input, Output: output, BaseURL: server.URL, Target: "zh-TW", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatal(requests)
	}
	data, _ := os.ReadFile(filepath.Join(output, "translated.srt"))
	if !strings.Contains(string(data), "00:00:01,000 --> 00:00:03,000\n啟動") {
		t.Fatal(string(data))
	}
	if err := RunSubtitleDiagnostic(context.Background(), SubtitleOptions{Input: input, Output: output, BaseURL: server.URL, Timeout: time.Second}); err == nil {
		t.Fatal("must refuse to overwrite evidence")
	}
}

func TestIconContainersAndGeometry(t *testing.T) {
	src := rasterIcon(contours{{{16, 16}, {48, 16}, {48, 48}, {16, 48}}})
	ico, err := icoBytes(src)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint16(ico[4:6]) != 6 {
		t.Fatal("ICO image count")
	}
	offset := binary.LittleEndian.Uint32(ico[18:22])
	img, err := png.Decode(bytes.NewReader(ico[offset:]))
	if err != nil || img.Bounds().Dx() != 16 {
		t.Fatalf("ICO PNG %v %v", img, err)
	}
	if !strings.Contains(svgMarkup(contours{{{16, 16}, {48, 16}, {48, 48}, {16, 48}}}), "fill-rule=\"evenodd\"") {
		t.Fatal("fill rule changed")
	}
}
