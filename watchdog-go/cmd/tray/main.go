// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"wuf-translate/watchdog-go/internal/launch"
	"wuf-translate/watchdog-go/internal/tray"
)

func run() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	root := filepath.Dir(executable)
	configPath := flag.String("config", filepath.Join(root, "launch.json"), "packaged launch config")
	assets := flag.String("assets", filepath.Join(root, "resources", "icons"), "packaged icon assets")
	swift := flag.String("swift", filepath.Join(root, "resources", "WUFMenuBar"), "retained macOS native UI executable")
	showPanel := flag.Bool("show-panel", false, "open the Windows left-click panel at startup")
	flag.Parse()
	c, err := launch.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	lockContext, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	hash := sha256.Sum256([]byte(c.BaseURL))
	release, err := launch.Acquire(lockContext, "tray-"+hex.EncodeToString(hash[:12]), filepath.Join(c.DataDir, "locks"))
	cancel()
	if errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	if err != nil {
		return err
	}
	defer release()
	health, err := launch.Probe(ctx, c.BaseURL)
	if err != nil {
		return err
	}
	return tray.Run(ctx, tray.NewClient(c.BaseURL), *assets, *swift, health.PID, *showPanel)
}
func main() {
	if err := run(); err != nil {
		log.New(os.Stderr, "WUF tray: ", 0).Print(err)
		os.Exit(1)
	}
}
