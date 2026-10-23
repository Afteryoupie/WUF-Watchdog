// SPDX-License-Identifier: Apache-2.0
package core

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
)

const DecisionModelID = "laya-multilingual"

type DecisionSnapshot struct {
	Status           string  `json:"status"`
	CurrentModel     string  `json:"current_model"`
	CurrentModelID   string  `json:"current_model_id"`
	Models           []Model `json:"models"`
	ActiveRequests   int     `json:"active_requests"`
	Device           string  `json:"device"`
	IdleTimeout      int     `json:"idle_timeout"`
	IdleRemaining    int     `json:"idle_remaining"`
	AutoSleepEnabled bool    `json:"auto_sleep_enabled"`
	RuntimeAvailable bool    `json:"runtime_available"`
	Endpoint         string  `json:"endpoint"`
	LastError        string  `json:"last_error"`
}

func ValidateDecision(payload map[string]any) error {
	if payload == nil || payload["state"] == nil {
		return errors.New("Expected a JSON object with state and questions")
	}
	if model, exists := payload["model"]; exists && model != DecisionModelID && model != "jev-latest" && model != "default" && model != "" {
		return errors.New("Unknown decision model")
	}
	questions, ok := payload["questions"].(map[string]any)
	if !ok || len(questions) < 1 || len(questions) > 32 {
		return errors.New("Expected 1–32 questions")
	}
	for key, value := range questions {
		question, ok := value.(map[string]any)
		if !ok || utf8.RuneCountInString(key) > 128 {
			return errors.New("Invalid question")
		}
		kind, ok := question["type"].(string)
		if !ok || kind != "noul" && kind != "choice" && kind != "score" {
			return errors.New("Question type must be noul, choice or score")
		}
		instructions, ok := question["instructions"].(string)
		if !ok || utf8.RuneCountInString(instructions) > 4000 {
			return errors.New("Expected question instructions up to 4000 characters")
		}
	}
	return nil
}

type Decision struct {
	manager                  *Manager
	cfg                      Config
	mu                       sync.Mutex
	token, device, lastError string
	client                   *http.Client
}

func NewDecision(c Config) (*Decision, error) {
	if err := validateConfig(c); err != nil {
		return nil, err
	}
	if c.DecisionPort < 1 || c.DecisionPort > 65535 || c.DecisionPort == c.InternalPort {
		return nil, errors.New("decision port must be valid and separate from the chat backend")
	}
	decisionConfig := c
	decisionConfig.InternalPort = c.DecisionPort
	decisionConfig.DataDir = filepath.Join(c.DataDir, "decision")
	models := []Model{}
	required := []string{"rl_agent_config.json", "model.safetensors", "encoder/config.json", "tokenizer/tokenizer.json", "tokenizer/tokenizer_config.json"}
	available := filepath.IsAbs(c.LayaModelDir)
	for _, file := range required {
		available = available && isFile(filepath.Join(c.LayaModelDir, filepath.FromSlash(file)))
	}
	if available {
		models = append(models, Model{ID: DecisionModelID, Name: "Laya multilingual", Path: c.LayaModelDir, Type: "laya", IdleTimeout: int(c.IdleTimeout.Seconds())})
	}
	d := &Decision{cfg: c, manager: managerForModels(decisionConfig, models, ""), client: &http.Client{
		Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	d.manager.start = func(ctx context.Context, model Model) (*child, error) {
		if !filepath.IsAbs(c.LayaRoot) || !isFile(filepath.Join(c.LayaRoot, "watchdog", "laya_backend.py")) {
			return nil, errors.New("configure WUF_LAYA_ROOT with the packaged Python backend directory")
		}
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", c.DecisionPort))
		if err != nil {
			return nil, fmt.Errorf("Laya port is occupied: %w", err)
		}
		if err := listener.Close(); err != nil {
			return nil, err
		}
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return nil, err
		}
		d.mu.Lock()
		d.token, d.lastError = hex.EncodeToString(token[:]), ""
		startupToken := d.token
		d.mu.Unlock()
		return launch(ctx, decisionConfig, []string{c.LayaPython, "-m", "watchdog.laya_backend", "--model", model.Path,
			"--port", strconv.Itoa(c.DecisionPort)}, c.LayaRoot,
			[]string{"LAYA_STARTUP_TOKEN=" + startupToken, "TRANSFORMERS_OFFLINE=1", "TOKENIZERS_PARALLELISM=false", "PYTHONUNBUFFERED=1"})
	}
	d.manager.probe = func(ctx context.Context, model Model) error {
		probe, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		data, err := d.http(probe, "GET", "/health", nil)
		if err != nil {
			return err
		}
		var health struct {
			Token  string `json:"startup_token"`
			Device string `json:"device"`
			Model  string `json:"model"`
		}
		if err := json.Unmarshal(data, &health); err != nil {
			return err
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.token == "" || health.Token != d.token || health.Model != DecisionModelID {
			return ErrBackendIdentity
		}
		d.device = health.Device
		return nil
	}
	return d, nil
}

func (d *Decision) http(ctx context.Context, method, path string, payload []byte) (json.RawMessage, error) {
	request, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("http://127.0.0.1:%d%s", d.cfg.DecisionPort, path), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := d.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 1024*1024 || !json.Valid(data) {
		return nil, errors.New("invalid Laya JSON response")
	}
	if response.StatusCode != 200 {
		var failure struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		json.Unmarshal(data, &failure)
		return nil, fmt.Errorf("Laya HTTP %d: %s", response.StatusCode, failure.Error.Message)
	}
	return data, nil
}

func (d *Decision) record(err error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		d.lastError = err.Error()
	} else {
		d.lastError = ""
	}
	return err
}

func (d *Decision) Snapshot() DecisionSnapshot {
	s := d.manager.Snapshot()
	d.mu.Lock()
	defer d.mu.Unlock()
	device := ""
	if s.Status == "running" {
		device = d.device
	}
	return DecisionSnapshot{Status: s.Status, CurrentModel: s.CurrentModel, CurrentModelID: s.CurrentModelID, Models: s.Models,
		ActiveRequests: s.ActiveRequests, Device: device, IdleTimeout: int(d.cfg.IdleTimeout.Seconds()), IdleRemaining: s.IdleRemaining,
		AutoSleepEnabled: s.AutoSleepEnabled, RuntimeAvailable: validateExecutable(d.cfg.LayaPython) == nil,
		Endpoint: "/v1/systemone", LastError: d.lastError}
}

func (d *Decision) Wake(ctx context.Context) error  { return d.record(d.manager.Wake(ctx)) }
func (d *Decision) Sleep(ctx context.Context) error { return d.record(d.manager.Sleep(ctx)) }
func (d *Decision) Stop() error                     { return d.manager.Stop() }
func (d *Decision) CheckIdle() error                { return d.manager.CheckIdle() }
func (d *Decision) ToggleAutoSleep()                { d.manager.SetAutoSleep(nil) }

func (d *Decision) Evaluate(ctx context.Context, payload map[string]any) (json.RawMessage, error) {
	if err := ValidateDecision(payload); err != nil {
		return nil, err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var result json.RawMessage
	err = d.manager.WithModel(ctx, "default", func(ctx context.Context, model Model) error {
		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		var err error
		result, err = d.http(ctx, "POST", "/v1/systemone", data)
		return err
	})
	return result, d.record(err)
}
