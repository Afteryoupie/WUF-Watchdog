// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
	"wuf-translate/watchdog-go/internal/launch"
	"wuf-translate/watchdog-go/internal/setup"
)

func logLauncherMessage(dataDir, message string) {
	if dataDir == "" {
		if root, lookupErr := os.UserConfigDir(); lookupErr == nil {
			dataDir = filepath.Join(root, "WUFTranslate")
		}
	}
	if dataDir != "" && os.MkdirAll(dataDir, 0700) == nil {
		if output, openErr := os.OpenFile(filepath.Join(dataDir, "launcher.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600); openErr == nil {
			log.New(output, "WUF app: ", log.LstdFlags).Print(message)
			output.Close()
		}
	}
}

func run() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	config := flag.String("config", filepath.Join(filepath.Dir(executable), "launch.json"), "packaged launch config")
	quiet := flag.Bool("quiet", false, "log errors without a dialog at login")
	flag.Parse()
	c, err := launch.LoadConfig(*config)
	if err == nil {
		if nativeErr := setup.EnsureNativeHost(executable); nativeErr != nil {
			logLauncherMessage(c.DataDir, "native host auto-register skipped: "+nativeErr.Error())
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		reply := launch.Start(ctx, c)
		if !reply.OK {
			err = fmt.Errorf("%s", reply.Error)
		}
	}
	if err != nil {
		logLauncherMessage(c.DataDir, err.Error())
	}
	if err != nil && !*quiet {
		showError(err.Error())
	}
	return err
}

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
