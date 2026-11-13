// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"wuf-translate/watchdog-go/internal/control"
	"wuf-translate/watchdog-go/internal/core"
)

// stripLegacyFlags drops removed Laya/Python CLI flags so upgraded launch.json
// files and desktop shortcuts continue to start cleanly.
func stripLegacyFlags(args []string) []string {
	removed := map[string]bool{
		"python": true, "rapid-mlx": true, "omlx": true,
		"laya-python": true, "laya-root": true, "laya-model": true, "decision-port": true,
	}
	cleaned := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			cleaned = append(cleaned, args[i:]...)
			break
		}
		if strings.HasPrefix(arg, "-") {
			trimmed := strings.TrimLeft(arg, "-")
			name, _, hasValue := strings.Cut(trimmed, "=")
			if removed[name] {
				if !hasValue && i+1 < len(args) {
					i++
				}
				continue
			}
		}
		cleaned = append(cleaned, arg)
	}
	return cleaned
}

func run() error {
	c, err := core.DefaultConfig()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	os.Args = append(os.Args[:1], stripLegacyFlags(os.Args[1:])...)
	listen := flag.String("listen", "127.0.0.1:8080", "loopback API address")
	dashboard := flag.String("dashboard", filepath.Join(filepath.Dir(executable), "resources", "dashboard.html"), "packaged dashboard resource")
	flag.StringVar(&c.ModelsDir, "models-dir", c.ModelsDir, "absolute model directory")
	flag.StringVar(&c.DataDir, "data-dir", c.DataDir, "absolute persistent data directory")
	flag.StringVar(&c.Binaries.Llama, "llama", c.Binaries.Llama, "absolute llama-server executable")
	flag.StringVar(&c.Binaries.K2Llama, "k2-llama", c.Binaries.K2Llama, "absolute K2 llama-server executable")
	flag.StringVar(&c.Binaries.FM, "fm", c.Binaries.FM, "absolute Apple fm executable")
	flag.StringVar(&c.Binaries.AppleAPI, "apple-api", c.Binaries.AppleAPI, "absolute Apple API executable")
	flag.IntVar(&c.InternalPort, "internal-port", c.InternalPort, "loopback chat backend port")
	flag.Parse()
	host, _, err := net.SplitHostPort(*listen)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return errors.New("API must listen on loopback")
	}
	manager, err := core.NewManager(c)
	if err != nil {
		return err
	}
	defer manager.Stop()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	address := listener.Addr().(*net.TCPAddr)
	if !address.IP.IsLoopback() {
		return errors.New("resolved API address is not loopback")
	}
	if address.Port == c.InternalPort {
		return errors.New("public and backend ports must be separate")
	}
	template, err := os.ReadFile(*dashboard)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) {
		log.Printf("dashboard resource is not installed: %s", *dashboard)
	}
	api := core.NewAPI(manager, string(template), address.Port)
	api.Autostart = func(ctx context.Context, enabled *bool) (control.AutostartStatus, error) {
		return control.Autostart(ctx, filepath.Join(filepath.Dir(executable), "launch.json"), enabled)
	}
	defer api.Close()
	server := core.HTTPServer(api)
	defer server.Close()
	serving := make(chan error, 1)
	go func() { serving <- core.Serve(server, listener) }()
	fmt.Printf("WUF Watchdog listening on http://%s\n", listener.Addr())
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-signals.Done():
			return api.Close()
		case <-api.Done():
			return api.Close()
		case err := <-serving:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-ticker.C:
			if err := manager.CheckIdle(); err != nil {
				log.Printf("chat idle: %v", err)
			}
		}
	}
}

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
