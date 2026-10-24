// SPDX-License-Identifier: Apache-2.0
package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"wuf-translate/watchdog-go/internal/launch"
)

func MenuChoice(input io.Reader, output io.Writer) (string, error) {
	fmt.Fprintln(output, "[1] 啟用開機自動啟動  [2] 取消開機自動啟動  [Q] 離開")
	fmt.Fprint(output, "請選擇：")
	scanner := bufio.NewScanner(input)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", nil
	}
	switch strings.ToLower(strings.TrimSpace(scanner.Text())) {
	case "1":
		return "enable-autostart", nil
	case "2":
		return "disable-autostart", nil
	case "q", "":
		return "", nil
	default:
		return "", errors.New("請輸入 1、2 或 Q；設定未變更")
	}
}

func AutostartCommand(configPath string) (string, error) {
	config, err := filepath.Abs(configPath)
	if err != nil {
		return "", err
	}
	app := filepath.Join(filepath.Dir(config), "app.exe")
	return `"` + app + `" --config "` + config + `" --quiet`, nil
}

func verifyOwner(c launch.Config, health launch.Health) error {
	actual, err := executablePath(health.PID)
	if err != nil {
		return fmt.Errorf("cannot verify WUF process: %w", err)
	}
	if !strings.EqualFold(filepath.Clean(actual), filepath.Clean(c.Core)) {
		return fmt.Errorf("refusing another WUF installation: %s", actual)
	}
	return nil
}

func Stop(ctx context.Context, c launch.Config) error {
	health, err := launch.Probe(ctx, c.BaseURL)
	if errors.Is(err, launch.ErrAbsent) {
		parsed, _ := url.Parse(c.BaseURL)
		connection, dialErr := net.DialTimeout("tcp", parsed.Host, time.Second)
		if dialErr == nil {
			connection.Close()
			return launch.ErrWrongService
		}
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	if err := verifyOwner(c, health); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/api/stop", nil)
	if err != nil {
		return err
	}
	client := http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var reply struct{ Status string }
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&reply) != nil || reply.Status != "stopped" {
		return fmt.Errorf("WUF did not acknowledge stop (HTTP %d)", response.StatusCode)
	}
	return waitForExit(ctx, health.PID)
}

// Run controls the packaged Go binaries directly. No shell or Python launchers.
func Run(ctx context.Context, configPath, action, testRegistryKey string, input io.Reader, output io.Writer) error {
	c, err := launch.LoadConfig(configPath)
	if err != nil {
		return err
	}
	action = strings.ToLower(action)
	if action == "startup-menu" {
		command, err := AutostartCommand(configPath)
		if err != nil {
			return err
		}
		value, found, err := readAutostart(testRegistryKey)
		if err != nil {
			return err
		}
		state := "未啟用"
		if found {
			state = "已啟用"
			if value != command && value != legacyNativeCommand(configPath) {
				state = "已有其他 WUF 登入命令（將保留）"
			}
		}
		fmt.Fprintln(output, "開機啟動："+state)
		action, err = MenuChoice(input, output)
		if err != nil {
			return err
		}
		if action == "" {
			return nil
		}
	}
	switch action {
	case "start":
		health, err := launch.Probe(ctx, c.BaseURL)
		if err == nil {
			if err := verifyOwner(c, health); err != nil {
				return err
			}
		} else if !errors.Is(err, launch.ErrAbsent) {
			return err
		}
		reply := launch.Start(ctx, c)
		if !reply.OK {
			return errors.New(reply.Error)
		}
		fmt.Fprintf(output, "WUF Watchdog 已啟動：%s (PID %d)\n", c.BaseURL, reply.PID)
	case "stop":
		if err := Stop(ctx, c); err != nil {
			return err
		}
		fmt.Fprintln(output, "WUF Watchdog 已關閉。")
	case "status":
		health, err := launch.Probe(ctx, c.BaseURL)
		if errors.Is(err, launch.ErrAbsent) {
			fmt.Fprintln(output, "Offline")
			return nil
		}
		if err != nil {
			return err
		}
		if err := verifyOwner(c, health); err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(health)
	case "enable-autostart", "disable-autostart":
		if action == "enable-autostart" {
			info, err := os.Stat(filepath.Join(filepath.Dir(configPath), "app.exe"))
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return errors.New("app.exe is not a regular file")
			}
		}
		command, err := AutostartCommand(configPath)
		if err != nil {
			return err
		}
		if err := setAutostart(testRegistryKey, command, legacyNativeCommand(configPath), action == "enable-autostart"); err != nil {
			return err
		}
		if action == "enable-autostart" {
			fmt.Fprintln(output, "已設定開機自動啟動：原生 Go app.exe（無 PowerShell／Python）。")
		} else {
			fmt.Fprintln(output, "已取消此 WUF 套件的開機自動啟動；其他登入項目保留。")
		}
	default:
		return errors.New("action must be start, stop, status, startup-menu, enable-autostart or disable-autostart")
	}
	return nil
}

// Accept the previous native installer command as owned, so cancelling an
// existing native login entry works before this CLI has ever enabled it.
func legacyNativeCommand(configPath string) string {
	absolute, err := filepath.Abs(configPath)
	if err != nil {
		return ""
	}
	return `"` + filepath.Join(filepath.Dir(absolute), "app.exe") + `" --quiet`
}
