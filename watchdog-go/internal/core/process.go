// SPDX-License-Identifier: Apache-2.0
package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type child struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error // read only after done closes
}

func watchProcess(cmd *exec.Cmd, closeLog func()) *child {
	p := &child{cmd: cmd, done: make(chan struct{})}
	go func() {
		p.err = cmd.Wait()
		closeLog()
		close(p.done)
	}()
	return p
}

func (p *child) stop() error {
	select {
	case <-p.done:
		return nil
	default:
	}
	err := killTree(p.cmd)
	select {
	case <-p.done:
		return nil
	case <-time.After(5 * time.Second):
		return errors.Join(err, fmt.Errorf("owned process %d did not exit after termination", p.cmd.Process.Pid))
	}
}

func startProcess(ctx context.Context, c Config, model Model) (*child, error) {
	argv, err := Command(c, model)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", c.InternalPort))
	if err != nil {
		return nil, fmt.Errorf("backend port is already occupied: %w", err)
	}
	if err := listener.Close(); err != nil {
		return nil, err
	}
	if model.Type == "apple" {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		check := exec.CommandContext(ctx, argv[0], "available", "--model", "system")
		configureProcess(check)
		if output, err := check.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("Apple model unavailable: %w: %s", err, output)
		}
	}
	return launch(ctx, c, argv, "", nil)
}

func launch(ctx context.Context, c Config, argv []string, dir string, env []string) (*child, error) {
	if err := validateExecutable(argv[0]); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(c.DataDir, 0700); err != nil {
		return nil, err
	}
	log, err := os.OpenFile(filepath.Join(c.DataDir, "model.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HF_HUB_OFFLINE=1", "TRANSFORMERS_VERBOSITY=error", "PYTHONWARNINGS=ignore::UserWarning")
	cmd.Env = append(cmd.Env, env...)
	cmd.Stdout, cmd.Stderr = log, log
	configureProcess(cmd)
	if err := ctx.Err(); err != nil {
		log.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		log.Close()
		return nil, err
	}
	return watchProcess(cmd, func() { log.Close() }), nil
}
