// SPDX-License-Identifier: Apache-2.0
package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type API struct {
	manager    *Manager
	decision   *Decision
	dashboard  string
	publicPort int
	transport  *http.Transport
	once       sync.Once
	done       chan struct{}
	closeError error // read after done closes
}

func NewAPI(manager *Manager, decision *Decision, dashboard string, publicPort int) *API {
	return &API{manager: manager, decision: decision, dashboard: dashboard, publicPort: publicPort,
		done: make(chan struct{}), transport: &http.Transport{Proxy: nil, DisableKeepAlives: true,
			DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext}}
}

func (a *API) Done() <-chan struct{} { return a.done }
func (a *API) Close() error {
	a.once.Do(func() {
		// Stop both concurrently: a busy chat must not delay Laya cancellation.
		var chatError, decisionError error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); chatError = a.manager.Stop() }()
		go func() { defer wg.Done(); decisionError = a.decision.Stop() }()
		wg.Wait()
		a.closeError = errors.Join(chatError, decisionError)
		a.transport.CloseIdleConnections()
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

type statusSnapshot struct {
	Snapshot
	Decision DecisionSnapshot `json:"decision"`
}

func (a *API) status() statusSnapshot {
	return statusSnapshot{a.manager.Snapshot(), a.decision.Snapshot()}
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

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := responseContext(r)
	defer cancel()
	r = r.WithContext(ctx)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	path, method := r.URL.Path, r.Method
	if method == "OPTIONS" {
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")
		w.WriteHeader(204)
		return
	}
	if path == "/v1/systemone" {
		if method != "POST" {
			writeError(w, 405, "Use POST for typed decisions")
			return
		}
		if len(r.TransferEncoding) != 0 || r.ContentLength <= 0 || r.ContentLength > 256*1024 {
			writeError(w, 400, "Expected Content-Length up to 256 KiB")
			return
		}
		payload, err := readObject(w, r, 256*1024)
		if err == nil {
			err = ValidateDecision(payload)
		}
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		result, err := a.decision.Evaluate(r.Context(), payload)
		if err != nil {
			writeError(w, 503, err.Error())
			return
		}
		writeJSON(w, 200, result)
		return
	}
	if path == "/api/decision/status" && method == "GET" {
		writeJSON(w, 200, a.decision.Snapshot())
		return
	}
	if strings.HasPrefix(path, "/api/decision/") && (method == "GET" || method == "POST") {
		var err error
		switch path {
		case "/api/decision/wake":
			err = a.decision.Wake(r.Context())
		case "/api/decision/sleep":
			err = a.decision.Sleep(r.Context())
		case "/api/decision/toggle_auto_sleep":
			a.decision.ToggleAutoSleep()
		default:
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, ErrBusy) {
			writeError(w, 409, "決策模型正在處理請求，請稍後重試")
			return
		}
		if err != nil {
			writeJSON(w, 503, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"status": "ok", "decision": a.decision.Snapshot()})
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
			if err := a.manager.Refresh(); err != nil {
				writeError(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, a.status())
			return
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
		if strings.HasPrefix(path, "/v1/models/") {
			// net/http has already URL-decoded Path; do not decode a second time.
			writeJSON(w, 200, modelItem(strings.Trim(strings.TrimPrefix(path, "/v1/models/"), "/")))
			return
		}
	}
	if method == "GET" || method == "POST" {
		switch path {
		case "/api/wake", "/api/start":
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
	if path == "/api/stop" || path == "/stop" || path == "/shutdown" {
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
	if strings.HasPrefix(path, "/v1/") {
		a.inference(w, r)
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
	if err := a.manager.Refresh(); err != nil {
		writeError(w, 500, err.Error())
		return
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
	delete(payload, "wuf_target_language")
	delete(payload, "shinkansen_target_language")
	if target != "" && strings.Contains(strings.ToLower(model.ID), "riva-translate") {
		if messages, ok := payload["messages"].([]any); ok {
			for _, value := range messages {
				message, ok := value.(map[string]any)
				if !ok {
					continue
				}
				if text, ok := message["content"].(string); ok && message["role"] == "user" {
					message["content"] = "Translate this into " + target + ": " + text
				}
			}
		}
	}
	switch model.Type {
	case "apple-api":
		payload["model"] = "permissive"
	case "apple":
		payload["model"] = "system"
	case "omlx":
		payload["model"] = model.ID
	default:
		payload["model"] = model.Path
	}
}
