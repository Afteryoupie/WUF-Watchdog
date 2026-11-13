// SPDX-License-Identifier: Apache-2.0
package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"wuf-translate/watchdog-go/internal/control"
	"wuf-translate/watchdog-go/internal/setup"
)

type API struct {
	Autostart  func(context.Context, *bool) (control.AutostartStatus, error)
	manager    *Manager
	dashboard  string
	publicPort int
	transport  *http.Transport
	once       sync.Once
	done       chan struct{}
	closeError error // read after done closes
}

func NewAPI(manager *Manager, dashboard string, publicPort int) *API {
	transport := &http.Transport{
		Proxy:               nil,
		DisableKeepAlives:   false,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     60 * time.Second,
		DialContext:         (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
	}
	if manager != nil {
		manager.mu.Lock()
		manager.onStop = transport.CloseIdleConnections
		manager.mu.Unlock()
	}
	return &API{manager: manager, dashboard: dashboard, publicPort: publicPort,
		done: make(chan struct{}), transport: transport}
}

func (a *API) Done() <-chan struct{} { return a.done }
func (a *API) Close() error {
	a.once.Do(func() {
		if a.manager != nil {
			a.closeError = a.manager.Stop()
		}
		if a.transport != nil {
			a.transport.CloseIdleConnections()
		}
		close(a.done)
	})
	return a.closeError
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "JSON encoding failed", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(code)
	w.Write(data)
}

func writeError(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, map[string]any{"error": map[string]string{"message": message}})
}

func browserRedirect(w http.ResponseWriter, r *http.Request) bool {
	accept := r.Header.Get("Accept")
	if r.Method == "GET" && strings.Contains(accept, "text/html") && !strings.Contains(accept, "application/json") {
		w.Header().Set("Location", "/status")
		w.WriteHeader(303)
		return true
	}
	return false
}

func (a *API) status() Snapshot {
	return a.manager.Snapshot()
}

func readObject(w http.ResponseWriter, r *http.Request, limit int64) (map[string]any, error) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, err
	}
	if body == nil {
		return nil, errors.New("Expected a JSON object")
	}
	return body, nil
}

func (a *API) serveSetup(w http.ResponseWriter, r *http.Request) {
	w.Header().Del("Access-Control-Allow-Origin")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" && r.Method != "POST" {
		writeError(w, 405, "Use GET or POST")
		return
	}
	if !validateManagementRequest(w, r, false) {
		return
	}
	fmBin := ""
	if a.manager != nil {
		fmBin = a.manager.cfg.Binaries.FM
	}
	if r.Method == "GET" {
		if r.URL.Query().Has("action") {
			writeError(w, 405, "Use POST with application/json to change setup settings")
			return
		}
		writeJSON(w, 200, setup.QueryStatus(r.Context(), "", fmBin))
		return
	}
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if media != "application/json" {
		writeError(w, 415, "Expected application/json")
		return
	}
	body, err := readObject(w, r, 1024)
	action, valid := body["action"].(string)
	if err != nil || !valid || len(body) != 1 || strings.TrimSpace(action) == "" {
		writeJSON(w, 400, map[string]string{"status": "error", "message": "不支援的設定動作"})
		return
	}
	var actionErr error
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "enable-autostart":
		actionErr = setup.SetAutostart("", true)
	case "disable-autostart":
		actionErr = setup.SetAutostart("", false)
	case "install-native-host":
		actionErr = setup.EnsureNativeHost("")
	case "uninstall":
		actionErr = setup.Uninstall("")
	default:
		writeJSON(w, 400, map[string]string{"status": "error", "message": "不支援的設定動作"})
		return
	}
	if actionErr != nil {
		code := 500
		if strings.Contains(strings.ToLower(actionErr.Error()), "refusing") {
			code = 409
		}
		writeJSON(w, code, map[string]any{"status": "error", "message": actionErr.Error(), "setup": setup.QueryStatus(r.Context(), "", fmBin)})
		return
	}
	writeJSON(w, 200, map[string]any{"status": "ok", "setup": setup.QueryStatus(r.Context(), "", fmBin)})
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := responseContext(r)
	defer cancel()
	r = r.WithContext(ctx)
	path, method := r.URL.Path, r.Method
	if path == "/api/autostart" {
		a.serveAutostart(w, r)
		return
	}
	if path == "/api/setup" {
		a.serveSetup(w, r)
		return
	}
	if strings.HasPrefix(path, "/v1/") {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if method == "OPTIONS" {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "86400")
			w.WriteHeader(204)
			return
		}
		if method == "GET" {
			switch path {
			case "/v1/models", "/v1/models/":
				if err := a.manager.Refresh(); err != nil {
					writeError(w, 500, err.Error())
					return
				}
				items := []map[string]any{}
				for _, model := range a.manager.Snapshot().Models {
					items = append(items, modelItem(model.ID), modelItem(model.Path))
				}
				items = append(items, modelItem("default"))
				writeJSON(w, 200, map[string]any{"object": "list", "data": items})
				return
			}
			if strings.HasPrefix(path, "/v1/models/") {
				// net/http has already URL-decoded Path; do not decode a second time.
				writeJSON(w, 200, modelItem(strings.Trim(strings.TrimPrefix(path, "/v1/models/"), "/")))
				return
			}
		}
		a.inference(w, r)
		return
	}
	if method == "GET" {
		switch path {
		case "/health", "/api/health":
			s := a.manager.Snapshot()
			writeJSON(w, 200, map[string]any{"status": "ok", "llm_status": s.Status, "current_model": s.CurrentModel,
				"service": "wuf-translate", "protocol_version": 1, "pid": os.Getpid()})
			return
		case "/api/status":
			if !validateManagementRequest(w, r, true) {
				return
			}
			if err := a.manager.Refresh(); err != nil {
				writeError(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, a.status())
			return
		case "/", "/status":
			if err := a.manager.Refresh(); err != nil {
				writeError(w, 500, err.Error())
				return
			}
			if a.dashboard == "" {
				http.Error(w, "Dashboard resource is not installed", 503)
				return
			}
			data, err := json.Marshal(a.status())
			if err != nil {
				writeError(w, 500, err.Error())
				return
			}
			html := strings.ReplaceAll(a.dashboard, "__INITIAL_STATE_JSON__", string(data))
			html = strings.ReplaceAll(html, "__PUBLIC_PORT__", strconv.Itoa(a.publicPort))
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, html)
			return
		case "/favicon.ico":
			w.WriteHeader(204)
			return
		}
	}
	if method == "GET" || method == "POST" {
		switch path {
		case "/api/wake", "/api/start":
			if !validateManagementRequest(w, r, true) {
				return
			}
			err := a.manager.Wake(r.Context())
			if browserRedirect(w, r) {
				return
			}
			if err != nil {
				writeJSON(w, 200, map[string]string{"status": "error", "state": "failed"})
			} else {
				writeJSON(w, 200, map[string]string{"status": "ok", "state": "running"})
			}
			return
		case "/api/sleep":
			if !validateManagementRequest(w, r, true) {
				return
			}
			err := a.manager.Sleep(r.Context())
			if errors.Is(err, ErrBusy) {
				writeError(w, 409, "模型正被其他請求使用，請稍後重試")
				return
			}
			if err != nil {
				writeError(w, 503, err.Error())
				return
			}
			if browserRedirect(w, r) {
				return
			}
			writeJSON(w, 200, map[string]string{"status": "ok", "state": "sleeping"})
			return
		case "/api/switch":
			if !validateManagementRequest(w, r, true) {
				return
			}
			alias := r.URL.Query().Get("model")
			if alias == "" && r.ContentLength != 0 {
				if payload, err := readObject(w, r, 1024*1024); err == nil {
					alias, _ = payload["model"].(string)
				}
			}
			if alias == "" {
				writeJSON(w, 400, map[string]string{"error": "缺少 model 參數"})
				return
			}
			if err := a.manager.Refresh(); err != nil {
				writeError(w, 500, err.Error())
				return
			}
			err := a.manager.Switch(r.Context(), alias)
			if errors.Is(err, ErrBusy) {
				writeError(w, 409, "模型正被其他請求使用，請稍後重試")
				return
			}
			if browserRedirect(w, r) {
				return
			}
			status, message := "ok", ""
			if err != nil {
				status, message = "error", err.Error()
			} else {
				message = "已成功切換並啟動 " + a.manager.Snapshot().CurrentModel
			}
			writeJSON(w, 200, map[string]string{"status": status, "message": message, "current_model": a.manager.Snapshot().CurrentModel})
			return
		case "/api/toggle_auto_sleep", "/api/auto_sleep":
			if !validateManagementRequest(w, r, true) {
				return
			}
			var enabled *bool
			if values, ok := r.URL.Query()["enabled"]; ok && len(values) > 0 {
				value := strings.ToLower(values[0])
				on := value == "1" || value == "true" || value == "yes" || value == "on"
				enabled = &on
			}
			on := a.manager.SetAutoSleep(enabled)
			if browserRedirect(w, r) {
				return
			}
			writeJSON(w, 200, map[string]any{"status": "ok", "auto_sleep_enabled": on})
			return
		}
	}
	if (method == "GET" || method == "POST") && (path == "/api/stop" || path == "/stop" || path == "/shutdown") {
		if !validateManagementRequest(w, r, false) {
			return
		}
		if path == "/api/stop" {
			writeJSON(w, 200, map[string]string{"status": "stopped"})
		} else {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, "<!DOCTYPE html><title>WUF Watchdog</title><p>WUF Watchdog已停止</p>")
		}
		http.NewResponseController(w).Flush()
		go a.Close()
		return
	}
	if strings.HasPrefix(path, "/api/") && method == "OPTIONS" {
		writeError(w, 405, "Use GET or POST")
		return
	}
	http.NotFound(w, r)
}

func modelItem(id string) map[string]any {
	return map[string]any{"id": id, "object": "model", "created": 1700000000, "owned_by": "local-llm"}
}

func (a *API) inference(w http.ResponseWriter, r *http.Request) {
	if len(r.TransferEncoding) > 0 {
		writeError(w, 400, "Use Content-Length for inference requests")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16*1024*1024))
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	var payload map[string]any
	alias := ""
	if len(body) > 0 && strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		err := json.Unmarshal(body, &payload)
		if payload == nil || err != nil {
			writeError(w, 400, "Expected a JSON object with a string model")
			return
		}
		if value, exists := payload["model"]; exists && value != nil {
			var ok bool
			alias, ok = value.(string)
			if !ok {
				writeError(w, 400, "Expected a JSON object with a string model")
				return
			}
		}
	}
	if !a.manager.HasModel(alias) {
		if err := a.manager.Refresh(); err != nil {
			writeError(w, 500, err.Error())
			return
		}
	}
	err = a.manager.WithModel(r.Context(), alias, func(ctx context.Context, model Model) error {
		outBody := body
		if payload != nil {
			AdaptPayload(payload, model)
			var err error
			outBody, err = json.Marshal(payload)
			if err != nil {
				return err
			}
		}
		proxy := &httputil.ReverseProxy{Transport: a.transport, FlushInterval: -1,
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(&url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", a.manager.cfg.InternalPort)})
				pr.Out = pr.Out.WithContext(ctx)
				if model.Type == "apple-api" {
					pr.Out.URL.Path = "/api" + pr.Out.URL.Path
					pr.Out.URL.RawPath = ""
				}
				pr.Out.Body = io.NopCloser(bytes.NewReader(outBody))
				pr.Out.GetBody = func() (io.ReadCloser, error) {
					return io.NopCloser(bytes.NewReader(outBody)), nil
				}
				pr.Out.ContentLength = int64(len(outBody))
				pr.Out.Header.Del("Content-Length")
			},
			ModifyResponse: func(resp *http.Response) error { resp.Header.Set("Access-Control-Allow-Origin", "*"); return nil },
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				if ctx.Err() == nil {
					writeError(w, 502, "Cannot connect to LLM")
				}
			},
		}
		proxy.ServeHTTP(w, r)
		return nil
	})
	if err == nil || r.Context().Err() != nil {
		return
	}
	if errors.Is(err, ErrUnknownModel) {
		writeError(w, 400, err.Error())
		return
	}
	writeError(w, 503, err.Error())
}

func AdaptPayload(payload map[string]any, model Model) {
	target, _ := payload["wuf_target_language"].(string)
	if target == "" {
		target, _ = payload["shinkansen_target_language"].(string)
	}
	if target == "" {
		target, _ = payload["target_language"].(string)
	}
	delete(payload, "wuf_target_language")
	delete(payload, "shinkansen_target_language")
	delete(payload, "target_language")
	lowerID := strings.ToLower(model.ID)
	if target != "" && strings.Contains(lowerID, "riva-translate") {
		if messages, ok := payload["messages"].([]any); ok {
			for _, value := range messages {
				message, ok := value.(map[string]any)
				if !ok {
					continue
				}
				if text, ok := message["content"].(string); ok && message["role"] == "user" {
					if !strings.HasPrefix(text, "Translate this into ") {
						message["content"] = "Translate this into " + target + ": " + text
					}
				}
			}
		}
	}
	if strings.Contains(lowerID, "qwen3") {
		if _, hasKwargs := payload["chat_template_kwargs"]; !hasKwargs {
			if _, hasEnableThinking := payload["enable_thinking"]; !hasEnableThinking {
				payload["chat_template_kwargs"] = map[string]any{"enable_thinking": false}
			}
		}
	}
	switch model.Type {
	case "apple-api":
		payload["model"] = "permissive"
	case "apple":
		payload["model"] = "system"
	default:
		payload["model"] = model.Path
	}
}
