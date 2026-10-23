// SPDX-License-Identifier: Apache-2.0
package control

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMenuChoice(t *testing.T) {
	for input, want := range map[string]string{"1\r\n": "enable-autostart", "2\n": "disable-autostart", "Q\n": "", "q\n": "", "": ""} {
		var out bytes.Buffer
		got, err := MenuChoice(strings.NewReader(input), &out)
		if err != nil || got != want {
			t.Fatalf("%q: %q %v", input, got, err)
		}
		if !strings.Contains(out.String(), "[Q]") {
			t.Fatal("missing cancel choice")
		}
	}
	if _, err := MenuChoice(strings.NewReader("bad\n"), &bytes.Buffer{}); err == nil {
		t.Fatal("invalid choice must not mutate startup state")
	}
}
func TestAutostartUsesNativeAppAndExplicitConfig(t *testing.T) {
	config := filepath.Join(t.TempDir(), "package with spaces", "launch.json")
	command, err := AutostartCommand(config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(command, `app.exe" --config "`+config+`" --quiet`) {
		t.Fatal(command)
	}
	if strings.Contains(strings.ToLower(command), "powershell") || strings.Contains(strings.ToLower(command), "python") {
		t.Fatal(command)
	}
	if _, err := os.Stat(filepath.Dir(config)); !os.IsNotExist(err) {
		t.Fatal("command generation should not create files")
	}
}
