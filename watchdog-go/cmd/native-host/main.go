// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"wuf-translate/watchdog-go/internal/launch"
)

func run() error {
	// Chrome/Edge pass the caller origin as argv[1]. Explicit --config is
	// reserved for installation checks and isolated integration tests.
	configPath := ""
	if len(os.Args) == 3 && os.Args[1] == "--config" {
		configPath = os.Args[2]
	} else {
		if len(os.Args) < 2 || os.Args[1] != launch.ExtensionOrigin {
			return launch.WriteMessage(os.Stdout, launch.Reply{Error: "unrecognized extension origin"})
		}
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		configPath = filepath.Join(filepath.Dir(executable), "launch.json")
	}
	config, err := launch.LoadConfig(configPath)
	if err != nil {
		return launch.WriteMessage(os.Stdout, launch.Reply{Error: err.Error()})
	}
	for {
		data, err := launch.ReadMessage(os.Stdin)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return launch.WriteMessage(os.Stdout, launch.Reply{Error: err.Error()})
		}
		var request struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal(data, &request); err != nil || request.Action != "start" {
			if err := launch.WriteMessage(os.Stdout, launch.Reply{Error: "unsupported action"}); err != nil {
				return err
			}
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		reply := launch.Start(ctx, config)
		cancel()
		if err := launch.WriteMessage(os.Stdout, reply); err != nil {
			return err
		}
	}
}

func main() {
	if err := run(); err != nil {
		log.New(os.Stderr, "WUF native host: ", 0).Print(err)
		os.Exit(1)
	}
}
