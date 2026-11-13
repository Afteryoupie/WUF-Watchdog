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

func isLoopbackHost(hostPort string) bool {
	host := hostPort
	if h, _, err := net.SplitHostPort(hostPort); err == nil {
		host = h
	}
	return host == "127.0.0.1" || host == "::1" || host == "[::1]" || host == "localhost"
}

func validateManagementRequest(w http.ResponseWriter, r *http.Request, allowExtension bool) bool {
	// Management and OS settings routes never inherit the /v1 inference API's wildcard CORS.
	w.Header().Del("Access-Control-Allow-Origin")
	w.Header().Set("Cache-Control", "no-store")
	if !isLoopbackHost(r.Host) {
		writeError(w, 403, "Expected a local dashboard host")
		return false
	}
	extensionOrigin := false
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil {
			writeError(w, 403, "Only authorized local callers can access management endpoints")
			return false
		}
		switch u.Scheme {
		case "http":
			if u.Host != r.Host {
				writeError(w, 403, "Only the local dashboard can access management endpoints")
				return false
			}
		case "chrome-extension", "moz-extension", "safari-web-extension":
			if !allowExtension || u.Host == "" {
				writeError(w, 403, "Only the local dashboard can access management endpoints")
				return false
			}
			extensionOrigin = true
		default:
			writeError(w, 403, "Only authorized local callers can access management endpoints")
			return false
		}
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		if !extensionOrigin {
			writeError(w, 403, "Only the local dashboard can access management endpoints")
			return false
		}
	}
	return true
}

func (a *API) serveAutostart(w http.ResponseWriter, r *http.Request) {
	w.Header().Del("Access-Control-Allow-Origin")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" && r.Method != "POST" {
		writeError(w, 405, "Use GET or POST")
		return
	}
	if !validateManagementRequest(w, r, false) {
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
