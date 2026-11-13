// SPDX-License-Identifier: Apache-2.0
//go:build darwin

package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"wuf-translate/watchdog-go/internal/launch"
	"wuf-translate/watchdog-go/internal/tooling"
)

func findAppBundle(exePath string) (string, bool) {
	if exePath == "" {
		return "", false
	}
	abs, err := filepath.Abs(exePath)
	if err != nil {
		return "", false
	}
	macosDir := filepath.Dir(abs)
	contentsDir := filepath.Dir(macosDir)
	appDir := filepath.Dir(contentsDir)
	if filepath.Base(macosDir) == "MacOS" && filepath.Base(contentsDir) == "Contents" && filepath.Ext(appDir) == ".app" {
		return appDir, true
	}
	return "", false
}

func manifestPaths(home string) []string {
	return []string{
		filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "NativeMessagingHosts", launch.HostName+".json"),
		filepath.Join(home, "Library", "Application Support", "Microsoft Edge", "NativeMessagingHosts", launch.HostName+".json"),
	}
}

func agentPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", LoginLabel+".plist")
}

func launchctlBootout() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+LoginLabel)
	data, err := cmd.CombinedOutput()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && (exit.ExitCode() == 3 || exit.ExitCode() == 113) {
			return nil
		}
		return fmt.Errorf("launchctl failed, entry preserved: %w: %s", err, strings.TrimSpace(string(data)))
	}
	return nil
}

func ensureNativeHost(exePath string) error {
	appDir, ok := findAppBundle(exePath)
	if !ok {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return tooling.InstallMac("install", appDir, home, launchctlBootout)
}

func setAutostart(exePath string, enabled bool) error {
	appDir, ok := findAppBundle(exePath)
	if !ok {
		return errors.New("autostart requires running from WUF Watchdog.app")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	action := "disable-autostart"
	if enabled {
		action = "enable-autostart"
	}
	return tooling.InstallMac(action, appDir, home, launchctlBootout)
}

func uninstall(exePath string) error {
	appDir, ok := findAppBundle(exePath)
	if !ok {
		return errors.New("uninstall requires running from WUF Watchdog.app")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return tooling.InstallMac("uninstall", appDir, home, launchctlBootout)
}

func jsonMatches(path string, expected map[string]any) (exists bool, match bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return true, false, err
	}
	var actual map[string]any
	if err := json.Unmarshal(data, &actual); err != nil {
		return true, false, err
	}
	a, _ := json.Marshal(actual)
	b, _ := json.Marshal(expected)
	return true, bytes.Equal(a, b), nil
}

func plistMatches(path string, expected map[string]any) (exists bool, match bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return true, false, err
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	actual := map[string]any{}
	var inDict, inArray bool
	var key string
	var items []string
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "dict":
				inDict = true
			case "key":
				if inDict {
					var v string
					if decoder.DecodeElement(&v, &t) == nil {
						key = v
					}
				}
			case "array":
				inArray = true
				items = []string{}
			case "string":
				var v string
				if decoder.DecodeElement(&v, &t) == nil {
					if inArray {
						items = append(items, v)
					} else if key != "" {
						actual[key] = v
						key = ""
					}
				}
			case "true":
				if key != "" {
					actual[key] = true
					key = ""
				}
			case "false":
				if key != "" {
					actual[key] = false
					key = ""
				}
			}
		case xml.EndElement:
			if t.Name.Local == "array" && key != "" {
				actual[key] = items
				inArray = false
				key = ""
			}
		}
	}
	return true, reflect.DeepEqual(actual, expected), nil
}

func queryFM(ctx context.Context, fmBin string) (string, string) {
	if fmBin == "" {
		return "missing", ""
	}
	info, err := os.Stat(fmBin)
	if err != nil || !info.Mode().IsRegular() {
		return "missing", ""
	}
	fmCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(fmCtx, fmBin, "available", "--model", "system").CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if err == nil {
		return "available", msg
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "license") || strings.Contains(lower, "terms") {
		return "license_required", msg
	}
	if msg == "" {
		msg = err.Error()
	}
	return "unavailable", msg
}

func queryPlatformStatus(ctx context.Context, exePath, fmBin string) Status {
	fmStatus, fmMessage := queryFM(ctx, fmBin)
	appDir, ok := findAppBundle(exePath)
	if !ok {
		return Status{
			NativeHost: "unsupported",
			Autostart:  "unsupported",
			FMStatus:   fmStatus,
			FMMessage:  fmMessage,
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Status{
			NativeHost:       "unsupported",
			NativeHostDetail: err.Error(),
			Autostart:        "unsupported",
			FMStatus:         fmStatus,
			FMMessage:        fmMessage,
		}
	}

	expectedManifest := tooling.HostManifest(appDir)
	allInstalled := true
	nativeState := "installed"
	nativeDetail := ""
	for _, p := range manifestPaths(home) {
		exists, match, err := jsonMatches(p, expectedManifest)
		if err != nil {
			nativeState = "foreign"
			nativeDetail = err.Error()
			allInstalled = false
			break
		}
		if exists && !match {
			nativeState = "foreign"
			nativeDetail = p
			allInstalled = false
			break
		}
		if !exists {
			allInstalled = false
		}
	}
	if nativeState == "installed" && !allInstalled {
		nativeState = "not_installed"
	}

	autostartState := "disabled"
	autostartDetail := ""
	if exists, match, err := plistMatches(agentPath(home), tooling.LoginAgent(appDir)); err != nil {
		autostartState = "foreign"
		autostartDetail = err.Error()
	} else if exists {
		if match {
			autostartState = "enabled"
		} else {
			autostartState = "foreign"
			autostartDetail = agentPath(home)
		}
	}

	return Status{
		NativeHost:       nativeState,
		NativeHostDetail: nativeDetail,
		Autostart:        autostartState,
		AutostartDetail:  autostartDetail,
		FMStatus:         fmStatus,
		FMMessage:        fmMessage,
	}
}
