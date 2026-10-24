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
	"syscall"
	"time"

	"wuf-translate/watchdog-go/internal/core"
)

func run() error {
	c, err := core.DefaultConfig()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	listen := flag.String("listen", "127.0.0.1:8080", "loopback API address")
	dashboard := flag.String("dashboard", filepath.Join(filepath.Dir(executable), "resources", "dashboard.html"), "packaged dashboard resource")
	flag.StringVar(&c.ModelsDir, "models-dir", c.ModelsDir, "absolute model directory")
	flag.StringVar(&c.DataDir, "data-dir", c.DataDir, "absolute persistent data directory")
	flag.StringVar(&c.Binaries.Llama, "llama", c.Binaries.Llama, "absolute llama-server executable")
	flag.StringVar(&c.Binaries.K2Llama, "k2-llama", c.Binaries.K2Llama, "absolute K2 llama-server executable")
	flag.StringVar(&c.Binaries.Python, "python", c.Binaries.Python, "absolute MLX Python executable")
	flag.StringVar(&c.Binaries.Rapid, "rapid-mlx", c.Binaries.Rapid, "absolute rapid-mlx executable")
	flag.StringVar(&c.Binaries.OMLX, "omlx", c.Binaries.OMLX, "absolute omlx executable")
	flag.StringVar(&c.Binaries.FM, "fm", c.Binaries.FM, "absolute Apple fm executable")
	flag.StringVar(&c.Binaries.AppleAPI, "apple-api", c.Binaries.AppleAPI, "absolute Apple API executable")
	flag.StringVar(&c.LayaPython, "laya-python", c.LayaPython, "absolute Laya Python executable")
	flag.StringVar(&c.LayaRoot, "laya-root", c.LayaRoot, "absolute packaged Laya backend directory")
	flag.StringVar(&c.LayaModelDir, "laya-model", c.LayaModelDir, "absolute Laya model directory")
	flag.IntVar(&c.InternalPort, "internal-port", c.InternalPort, "loopback chat backend port")
	flag.IntVar(&c.DecisionPort, "decision-port", c.DecisionPort, "separate loopback Laya port")
	flag.Parse()
	if os.Getenv("WUF_LAYA_MODEL_DIR") == "" {
		explicit := false
		flag.Visit(func(f *flag.Flag) {
			if f.Name == "laya-model" {
				explicit = true
			}
		})
		if !explicit {
			c.LayaModelDir = filepath.Join(c.ModelsDir, "Laya-Multilingual")
		}
	}
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
	decision, err := core.NewDecision(c)
	if err != nil {
		return err
	}
	defer decision.Stop()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	address := listener.Addr().(*net.TCPAddr)
	if !address.IP.IsLoopback() {
		return errors.New("resolved API address is not loopback")
	}
	if address.Port == c.InternalPort || address.Port == c.DecisionPort {
		return errors.New("public and backend ports must be separate")
	}
	template, err := os.ReadFile(*dashboard)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) {
		log.Printf("dashboard resource is not installed: %s", *dashboard)
	}
	api := core.NewAPI(manager, decision, string(template), address.Port)
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
			if err := decision.CheckIdle(); err != nil {
				log.Printf("decision idle: %v", err)
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
