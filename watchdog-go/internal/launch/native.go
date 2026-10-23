// SPDX-License-Identifier: Apache-2.0
package launch

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const HostName = "com.wuf_translate.watchdog"
const ExtensionOrigin = "chrome-extension://iecdpfnjngfljdlpookikhidlbfbmidb/"

type Config struct {
	Core          string   `json:"core"`
	Tray          string   `json:"tray,omitempty"`
	Arguments     []string `json:"arguments,omitempty"`
	TrayArguments []string `json:"tray_arguments,omitempty"`
	BaseURL       string   `json:"base_url"`
	DataDir       string   `json:"data_dir"`
}

type Reply struct {
	OK           bool   `json:"ok"`
	Already      bool   `json:"already,omitempty"`
	ServiceReady bool   `json:"service_ready"`
	ModelReady   bool   `json:"model_ready"`
	PID          int    `json:"pid,omitempty"`
	Error        string `json:"error,omitempty"`
}

func ReadMessage(reader io.Reader) ([]byte, error) {
	var length uint32
	if err := binary.Read(reader, binary.LittleEndian, &length); err != nil {
		return nil, err
	}
	if length == 0 || length > 65536 {
		return nil, errors.New("native message must contain 1–65536 UTF-8 bytes")
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, err
	}
	if !utf8.Valid(data) || !json.Valid(data) {
		return nil, errors.New("invalid native JSON message")
	}
	return data, nil
}

func WriteMessage(writer io.Writer, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if len(data) > 1024*1024 {
		return errors.New("native response exceeds 1 MiB")
	}
	frame := make([]byte, 4+len(data))
	binary.LittleEndian.PutUint32(frame, uint32(len(data)))
	copy(frame[4:], data)
	for len(frame) > 0 {
		n, err := writer.Write(frame)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		frame = frame[n:]
	}
	return nil
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return c, err
	}
	root, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return c, err
	}
	resolve := func(value string) string {
		if value == "" || filepath.IsAbs(value) {
			return value
		}
		return filepath.Join(root, value)
	}
	c.Core, c.Tray = resolve(c.Core), resolve(c.Tray)
	// Only documented path flags are bundle-relative; other arguments remain literal.
	pathFlags := map[string]bool{"--dashboard": true, "--laya-root": true, "--models-dir": true, "--laya-model": true, "--llama": true, "--k2-llama": true, "--python": true, "--laya-python": true, "--rapid-mlx": true, "--omlx": true, "--fm": true, "--apple-api": true, "--config": true, "--assets": true, "--swift": true}
	for _, args := range [][]string{c.Arguments, c.TrayArguments} {
		for i := 0; i < len(args); i++ {
			name, value, joined := strings.Cut(args[i], "=")
			if !pathFlags[name] {
				continue
			}
			if joined {
				args[i] = name + "=" + resolve(value)
			} else if i+1 < len(args) {
				i++
				args[i] = resolve(args[i])
			} else {
				return c, fmt.Errorf("missing value for %s", name)
			}
		}
	}
	if c.DataDir == "" {
		data, err := os.UserConfigDir()
		if err != nil {
			return c, err
		}
		c.DataDir = filepath.Join(data, "WUFTranslate")
	}
	if !filepath.IsAbs(c.DataDir) {
		return c, errors.New("launch data directory must be absolute")
	}
	if c.BaseURL == "" {
		c.BaseURL = "http://127.0.0.1:8080"
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return c, err
	}
	if u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return c, errors.New("launch API must be a plain loopback HTTP origin")
	}
	if u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return c, errors.New("launch API must use a loopback IP literal")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return c, errors.New("launch API requires an explicit valid port")
	}
	if _, _, err := net.SplitHostPort(u.Host); err != nil {
		return c, err
	}
	c.BaseURL = strings.TrimSuffix(c.BaseURL, "/")
	for _, executable := range []string{c.Core, c.Tray} {
		if executable == "" {
			continue
		}
		if !filepath.IsAbs(executable) {
			return c, errors.New("launch executable path must be absolute")
		}
		switch strings.ToLower(filepath.Ext(executable)) {
		case ".bat", ".cmd", ".command", ".ps1":
			return c, errors.New("launch directly through core executables")
		}
	}
	if c.Core == "" {
		return c, errors.New("core executable is required")
	}
	return c, nil
}

var ErrAbsent = errors.New("WUF control API is not reachable")
var ErrWrongService = errors.New("loopback port belongs to another service")

type Health struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version int    `json:"protocol_version"`
	PID     int    `json:"pid"`
	State   string `json:"llm_status"`
}

func Probe(ctx context.Context, baseURL string) (Health, error) {
	request, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/api/health", nil)
	if err != nil {
		return Health{}, err
	}
	client := http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return Health{}, fmt.Errorf("%w: %v", ErrAbsent, err)
	}
	defer response.Body.Close()
	var health Health
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&health) != nil || health.Status != "ok" || health.Service != "wuf-translate" || health.Version != 1 || health.PID <= 0 {
		return health, ErrWrongService
	}
	return health, nil
}

func spawn(executable string, args []string, dataDir string) (*os.Process, <-chan error, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, nil, err
	}
	log, err := os.OpenFile(filepath.Join(dataDir, "launcher.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, nil, err
	}
	create := func(breakaway bool) *exec.Cmd {
		cmd := exec.Command(executable, args...)
		cmd.Dir = filepath.Dir(executable)
		cmd.Stdout, cmd.Stderr = log, log
		detach(cmd, breakaway)
		return cmd
	}
	cmd := create(true)
	if err := cmd.Start(); err != nil {
		cmd = create(false)
		if err := cmd.Start(); err != nil {
			log.Close()
			return nil, nil, err
		}
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait(); log.Close(); close(done) }()
	return cmd.Process, done, nil
}

func Start(ctx context.Context, c Config) Reply {
	hash := sha256.Sum256([]byte(c.BaseURL))
	release, err := Acquire(ctx, "launch-"+hex.EncodeToString(hash[:12]), filepath.Join(c.DataDir, "locks"))
	if err != nil {
		return Reply{Error: err.Error()}
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return Reply{Error: err.Error()}
	}
	health, err := Probe(ctx, c.BaseURL)
	already := err == nil
	if errors.Is(err, ErrWrongService) {
		return Reply{Error: err.Error()}
	}
	if err != nil {
		if err := ctx.Err(); err != nil {
			return Reply{Error: err.Error()}
		}
		parsed, parseErr := url.Parse(c.BaseURL)
		if parseErr != nil {
			return Reply{Error: parseErr.Error()}
		}
		args := append(append([]string{}, c.Arguments...), "--listen", parsed.Host, "--data-dir", c.DataDir)
		process, exited, err := spawn(c.Core, args, c.DataDir)
		if err != nil {
			return Reply{Error: err.Error()}
		}
		ready := false
		defer func() {
			if !ready {
				process.Kill()
				select {
				case <-exited:
				case <-time.After(2 * time.Second):
				}
			}
		}()
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			health, err = Probe(ctx, c.BaseURL)
			if err == nil {
				if health.PID != process.Pid {
					return Reply{Error: "ready API PID does not match the launched core"}
				}
				ready = true
				break
			}
			if errors.Is(err, ErrWrongService) {
				return Reply{Error: err.Error()}
			}
			select {
			case <-ctx.Done():
				return Reply{Error: "control API startup timed out: " + ctx.Err().Error()}
			case exitError := <-exited:
				return Reply{Error: fmt.Sprintf("core exited before API readiness: %v", exitError)}
			case <-ticker.C:
			}
		}
	}
	if c.Tray != "" {
		if _, _, err := spawn(c.Tray, c.TrayArguments, c.DataDir); err != nil {
			return Reply{PID: health.PID, ServiceReady: true, Error: err.Error()}
		}
	}
	return Reply{OK: true, Already: already, ServiceReady: true, ModelReady: health.State == "running", PID: health.PID}
}
