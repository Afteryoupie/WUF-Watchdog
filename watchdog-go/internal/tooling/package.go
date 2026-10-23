// SPDX-License-Identifier: Apache-2.0
package tooling

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"

	"wuf-translate/watchdog-go/internal/launch"
)

const LoginLabel = "local.wuf.translate.watchdog"

func copyFile(source, target string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	return errors.Join(writeErr, closeErr)
}

func writeNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	return errors.Join(writeErr, f.Close())
}

// Plist emits the small XML property-list vocabulary used by our native bundle.
func Plist(values map[string]any) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\"><dict>")
	var keys []string
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	escaped := func(s string) { _ = xml.EscapeText(&b, []byte(s)) }
	for _, key := range keys {
		b.WriteString("<key>")
		escaped(key)
		b.WriteString("</key>")
		switch value := values[key].(type) {
		case string:
			b.WriteString("<string>")
			escaped(value)
			b.WriteString("</string>")
		case bool:
			if value {
				b.WriteString("<true/>")
			} else {
				b.WriteString("<false/>")
			}
		case []string:
			b.WriteString("<array>")
			for _, s := range value {
				b.WriteString("<string>")
				escaped(s)
				b.WriteString("</string>")
			}
			b.WriteString("</array>")
		default:
			return nil, fmt.Errorf("unsupported property-list value for %s", key)
		}
	}
	b.WriteString("</dict></plist>\n")
	return b.Bytes(), nil
}

func HostManifest(app string) map[string]any {
	return map[string]any{"name": launch.HostName, "description": "WUF Translate native launcher", "type": "stdio", "path": filepath.Join(app, "Contents", "MacOS", "native-host"), "allowed_origins": []string{launch.ExtensionOrigin}}
}
func LoginAgent(app string) map[string]any {
	return map[string]any{"Label": LoginLabel, "ProgramArguments": []string{filepath.Join(app, "Contents", "MacOS", "app"), "--quiet"}, "RunAtLoad": true, "ProcessType": "Background"}
}

// PopulateMac creates only a new directory; failed builds remain inspectable.
func PopulateMac(repo, app string) error {
	if !filepath.IsAbs(repo) || !filepath.IsAbs(app) || filepath.Ext(app) != ".app" {
		return errors.New("absolute repository and new .app paths are required")
	}
	if err := os.MkdirAll(filepath.Dir(app), 0755); err != nil {
		return err
	}
	if err := os.Mkdir(app, 0755); err != nil {
		return err
	}
	macos := filepath.Join(app, "Contents", "MacOS")
	resources := filepath.Join(app, "Contents", "Resources")
	for _, dir := range []string{macos, resources, filepath.Join(resources, "laya", "watchdog")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	files := map[string]string{
		"watchdog/templates/dashboard.html": "dashboard.html", "watchdog/assets/guardian.icns": "guardian.icns", "watchdog-go/LICENSE": "LICENSE",
	}
	for _, name := range []string{"__init__.py", "decision_protocol.py", "laya_backend.py"} {
		files["watchdog/"+name] = "laya/watchdog/" + name
	}
	for source, target := range files {
		if err := copyFile(filepath.Join(repo, filepath.FromSlash(source)), filepath.Join(resources, filepath.FromSlash(target))); err != nil {
			return err
		}
	}
	config := launch.Config{Core: "watchdog", Tray: "tray", BaseURL: "http://127.0.0.1:8080", Arguments: []string{"--dashboard", "../Resources/dashboard.html", "--laya-root", "../Resources/laya"}, TrayArguments: []string{"--config", "launch.json", "--swift", "../Resources/LLMMenuBar"}}
	data, _ := json.MarshalIndent(config, "", "  ")
	if err := writeNew(filepath.Join(macos, "launch.json"), data); err != nil {
		return err
	}
	info, err := Plist(map[string]any{"CFBundleName": "LLM watchdog", "CFBundleDisplayName": "LLM watchdog", "CFBundleIdentifier": LoginLabel, "CFBundleExecutable": "app", "CFBundleIconFile": "guardian.icns", "CFBundlePackageType": "APPL", "CFBundleVersion": "1", "LSUIElement": true, "NSHighResolutionCapable": true})
	if err != nil {
		return err
	}
	return writeNew(filepath.Join(app, "Contents", "Info.plist"), info)
}

func run(executable, cwd string, env []string, args ...string) error {
	if !filepath.IsAbs(executable) {
		return errors.New("an absolute core executable path is required")
	}
	switch strings.ToLower(filepath.Ext(executable)) {
	case ".bat", ".cmd", ".command":
		return errors.New("shell launch shortcuts are not core executables")
	}
	command := exec.Command(executable, args...)
	command.Dir = cwd
	command.Env = append(os.Environ(), env...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

func BuildMac(repo, app, goExe, swiftc string, layoutOnly bool) error {
	if !layoutOnly && runtime.GOOS != "darwin" {
		return errors.New("compile and sign on native macOS; --layout-only is available elsewhere")
	}
	if !layoutOnly && (!filepath.IsAbs(goExe) || !filepath.IsAbs(swiftc)) {
		return errors.New("absolute Go and Swift compiler paths are required")
	}
	if err := PopulateMac(repo, app); err != nil {
		return err
	}
	if layoutOnly {
		return nil
	}
	macos := filepath.Join(app, "Contents", "MacOS")
	resources := filepath.Join(app, "Contents", "Resources")
	for _, entry := range []string{"watchdog", "native-host", "tray", "app", "wuf-tool"} {
		if err := run(goExe, filepath.Join(repo, "watchdog-go"), []string{"GOOS=darwin", "CGO_ENABLED=0"}, "build", "-trimpath", "-buildvcs=false", "-o", filepath.Join(macos, entry), "./cmd/"+entry); err != nil {
			return err
		}
	}
	if err := run(swiftc, repo, nil, "-O", filepath.Join(repo, "watchdog", "LLMMenuBar.swift"), "-o", filepath.Join(resources, "LLMMenuBar")); err != nil {
		return err
	}
	cmd := exec.Command(goExe, "env", "GOROOT")
	root, err := cmd.Output()
	if err != nil {
		return err
	}
	if err := copyFile(filepath.Join(strings.TrimSpace(string(root)), "LICENSE"), filepath.Join(resources, "GO-LICENSE.txt")); err != nil {
		return err
	}
	if err := writeNew(filepath.Join(resources, "NOTICE.txt"), []byte("First-party code: Apache-2.0. Go runtime: BSD-3-Clause. Models and inference runtimes supplied separately. Python is used only by the isolated Laya backend.\n")); err != nil {
		return err
	}
	return run("/usr/bin/codesign", repo, nil, "--force", "--deep", "--sign", "-", app)
}

// Parse only the XML types we write. Foreign/binary plists are refused, never replaced.
func readPlist(data []byte) (map[string]any, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	result := map[string]any{}
	key := ""
	for {
		token, err := d.Token()
		if err == io.EOF {
			return result, nil
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "key":
			if err := d.DecodeElement(&key, &start); err != nil {
				return nil, err
			}
		case "string":
			var value string
			if err := d.DecodeElement(&value, &start); err != nil {
				return nil, err
			}
			result[key] = value
		case "true":
			result[key] = true
		case "false":
			result[key] = false
		case "array":
			var values struct {
				Strings []string `xml:"string"`
			}
			if err := d.DecodeElement(&values, &start); err != nil {
				return nil, err
			}
			result[key] = values.Strings
		}
	}
}

func matches(path string, expected map[string]any, plist bool) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	var actual map[string]any
	if plist {
		actual, err = readPlist(data)
	} else {
		err = json.Unmarshal(data, &actual)
		if err == nil {
			a, _ := json.Marshal(actual)
			b, _ := json.Marshal(expected)
			return bytes.Equal(a, b), nil
		}
	}
	return err == nil && reflect.DeepEqual(actual, expected), err
}

// InstallMac never changes an unrelated registration, login entry, model or setting.
func InstallMac(action, app, home string, bootout func() error) (err error) {
	expected := HostManifest(app)
	agent := filepath.Join(home, "Library", "LaunchAgents", LoginLabel+".plist")
	manifests := []string{}
	for _, browser := range []string{"Google/Chrome", "Microsoft Edge"} {
		manifests = append(manifests, filepath.Join(home, "Library", "Application Support", filepath.FromSlash(browser), "NativeMessagingHosts", launch.HostName+".json"))
	}
	if action == "install" || action == "enable-autostart" {
		for _, name := range []string{"app", "native-host", "tray", "watchdog"} {
			info, err := os.Stat(filepath.Join(app, "Contents", "MacOS", name))
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0) {
				return fmt.Errorf("missing native executable: %s", name)
			}
		}
	}
	switch action {
	case "install":
		for _, p := range manifests {
			ok, err := matches(p, expected, false)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("refusing foreign registration: %s", p)
			}
		}
		data, _ := json.MarshalIndent(expected, "", "  ")
		var added []string
		defer func() {
			if err != nil {
				for _, created := range added {
					if ok, _ := matches(created, expected, false); ok {
						_ = os.Remove(created)
					}
				}
			}
		}()
		for _, p := range manifests {
			if _, err := os.Stat(p); err == nil {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
				return err
			}
			if err := writeNew(p, data); err != nil {
				return err
			}
			added = append(added, p)
		}
	case "enable-autostart":
		ok, err := matches(agent, LoginAgent(app), true)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("refusing foreign LaunchAgent")
		}
		if _, err := os.Stat(agent); err == nil {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(agent), 0755); err != nil {
			return err
		}
		data, err := Plist(LoginAgent(app))
		if err != nil {
			return err
		}
		return writeNew(agent, data)
	case "uninstall", "disable-autostart":
		if action == "uninstall" {
			for _, p := range manifests {
				if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
					continue
				}
				ok, err := matches(p, expected, false)
				if err != nil {
					return err
				}
				if ok {
					if err := os.Remove(p); err != nil {
						return err
					}
				}
			}
		}
		if _, err := os.Stat(agent); errors.Is(err, os.ErrNotExist) {
			return nil
		}
		ok, err := matches(agent, LoginAgent(app), true)
		if err != nil {
			return err
		}
		if ok {
			if err := bootout(); err != nil {
				return err
			}
			return os.Remove(agent)
		}
	default:
		return errors.New("action must be install, uninstall, enable-autostart or disable-autostart")
	}
	return nil
}
