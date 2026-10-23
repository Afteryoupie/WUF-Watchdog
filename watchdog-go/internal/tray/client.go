// SPDX-License-Identifier: Apache-2.0
package tray

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"wuf-translate/watchdog-go/internal/core"
)

type Snapshot struct {
	core.Snapshot
	Decision core.DecisionSnapshot `json:"decision"`
}

func Compact(value string) string {
	characters := []rune(value)
	if len(characters) > 25 {
		value = string(characters[:14]) + "…" + string(characters[len(characters)-10:])
	}
	return value
}

func IconState(data *Snapshot) string {
	if data == nil {
		return "offline"
	}
	state := data.Status
	if state != "running" && state != "starting" {
		state = data.Decision.Status
	}
	if state == "starting" {
		return "starting"
	}
	if state == "running" {
		if data.ActiveRequests > 0 || data.Decision.ActiveRequests > 0 {
			return "running"
		}
		return "ready"
	}
	return "sleeping"
}

func Countdown(data *Snapshot, elapsed time.Duration) string {
	if data == nil {
		return "尚未連線"
	}
	if data.Status == "starting" {
		return "載入中…"
	}
	if data.Status != "running" {
		return "休眠中"
	}
	if data.ActiveRequests > 0 {
		return "處理中…"
	}
	if !data.AutoSleepEnabled || data.IdleTimeout <= 0 {
		return "保持喚醒"
	}
	remaining := max(0, data.IdleRemaining-int(elapsed.Seconds()))
	return fmt.Sprintf("%02d:%02d", remaining/60, remaining%60)
}

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{BaseURL: strings.TrimSuffix(baseURL, "/"), HTTP: &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *Client) Status(ctx context.Context) (*Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+"/api/status", nil)
	if err != nil {
		return nil, err
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("status HTTP %d", response.StatusCode)
	}
	var state Snapshot
	if err := json.NewDecoder(http.MaxBytesReader(nil, response.Body, 1024*1024)).Decode(&state); err != nil {
		return nil, err
	}
	if state.Status != "sleeping" && state.Status != "starting" && state.Status != "running" {
		return nil, errors.New("invalid Watchdog status")
	}
	return &state, nil
}

func (c *Client) Action(ctx context.Context, path string, query url.Values) error {
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	endpoint := c.BaseURL + "/api/" + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, "POST", endpoint, nil)
	if err != nil {
		return err
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var result struct {
		Status  string `json:"status"`
		State   string `json:"state"`
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, response.Body, 1024*1024)).Decode(&result); err != nil {
		return err
	}
	if response.StatusCode != 200 || result.Status == "error" {
		message := result.Error.Message
		if message == "" {
			message = result.Message
		}
		if message == "" {
			message = "操作未完成：" + result.State
		}
		return errors.New(message)
	}
	return nil
}
