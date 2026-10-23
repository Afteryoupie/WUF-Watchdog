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
)

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
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		reply := launch.Start(ctx, c)
		if !reply.OK {
			err = fmt.Errorf("%s", reply.Error)
		}
	}
	if err != nil {
		data := c.DataDir
		if data == "" {
			if root, lookupErr := os.UserConfigDir(); lookupErr == nil {
				data = filepath.Join(root, "WUFTranslate")
			}
		}
		if data != "" && os.MkdirAll(data, 0700) == nil {
			if output, openErr := os.OpenFile(filepath.Join(data, "launcher.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600); openErr == nil {
				log.New(output, "WUF app: ", log.LstdFlags).Print(err)
				output.Close()
			}
		}
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
