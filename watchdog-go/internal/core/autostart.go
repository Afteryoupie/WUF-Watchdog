// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"mime"
	"net"
	"net/http"
	"net/url"
	"time"

	"wuf-translate/watchdog-go/internal/control"
)

func (a *API) serveAutostart(w http.ResponseWriter, r *http.Request) {
	// OS settings are dashboard-only: do not inherit the inference API's CORS.
	w.Header().Del("Access-Control-Allow-Origin")
	w.Header().Set("Cache-Control", "no-store")
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil || (host != "127.0.0.1" && host != "::1" && host != "localhost") {
		writeError(w, 403, "Expected a local dashboard host")
		return
	}
	if r.Method != "GET" && r.Method != "POST" {
		writeError(w, 405, "Use GET or POST")
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != "http" || u.Host != r.Host {
			writeError(w, 403, "Only the local dashboard can change startup settings")
			return
		}
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		writeError(w, 403, "Only the local dashboard can access startup settings")
		return
	}
	var enabled *bool
	if r.Method == "POST" {
		media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if media != "application/json" {
			writeError(w, 415, "Expected application/json")
			return
		}
		body, err := readObject(w, r, 1024)
		value, valid := body["enabled"].(bool)
		if err != nil || !valid || len(body) != 1 {
			writeError(w, 400, "Expected a boolean enabled value")
			return
		}
		enabled = &value
	}
	if a.Autostart == nil {
		writeJSON(w, 200, control.AutostartStatus{})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	state, err := a.Autostart(ctx, enabled)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, state)
}
