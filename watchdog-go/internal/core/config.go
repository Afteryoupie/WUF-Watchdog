// SPDX-License-Identifier: Apache-2.0
package core

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Binaries struct {
	Llama, K2Llama, Python, Rapid, OMLX, FM, AppleAPI string
}

type Config struct {
	ModelsDir, DataDir                 string
	Platform                           string
	Binaries                           Binaries
	InternalPort                       int
	VRAMMiB                            int64
	StartupTimeout                     time.Duration
	QueueTimeout                       time.Duration
	IdleTimeout                        time.Duration
	ProbeInterval                      time.Duration
	LayaModelDir, LayaRoot, LayaPython string
	DecisionPort                       int
}

func DefaultConfig() (Config, error) {
	data, err := os.UserConfigDir()
	if err != nil {
		return Config{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}
	c := Config{
		ModelsDir: filepath.Join(home, "LLM", "Models"), DataDir: filepath.Join(data, "WUFTranslate"),
		Platform: runtime.GOOS, InternalPort: 8081, VRAMMiB: 12224,
		StartupTimeout: 120 * time.Second, QueueTimeout: 130 * time.Second,
		IdleTimeout: 180 * time.Second, ProbeInterval: 100 * time.Millisecond,
		DecisionPort: 8082,
	}
	if value := os.Getenv("WUF_MODELS_DIR"); value != "" {
		c.ModelsDir = value
	}
	if value := os.Getenv("WUF_DATA_DIR"); value != "" {
		c.DataDir = value
	}
	c.Binaries.Llama, _ = exec.LookPath("llama-server")
	c.Binaries.K2Llama = os.Getenv("WUF_K2_LLAMA")
	c.Binaries.Python = os.Getenv("WUF_PYTHON")
	c.Binaries.Rapid = os.Getenv("WUF_RAPID_MLX")
	c.Binaries.OMLX = os.Getenv("WUF_OMLX")
	c.Binaries.AppleAPI = os.Getenv("WUF_APPLE_API")
	c.LayaModelDir = os.Getenv("WUF_LAYA_MODEL_DIR")
	if c.LayaModelDir == "" {
		c.LayaModelDir = filepath.Join(c.ModelsDir, "Laya-Multilingual")
	}
	c.LayaRoot = os.Getenv("WUF_LAYA_ROOT")
	c.LayaPython = os.Getenv("WUF_LAYA_PYTHON")
	if c.LayaPython == "" {
		c.LayaPython = c.Binaries.Python
	}
	if runtime.GOOS == "darwin" {
		c.Binaries.FM = "/usr/bin/fm"
	}
	return c, nil
}

func validateConfig(c Config) error {
	if !filepath.IsAbs(c.ModelsDir) || !filepath.IsAbs(c.DataDir) {
		return errors.New("absolute model and data directories are required")
	}
	if c.StartupTimeout <= 0 || c.QueueTimeout <= 0 || c.ProbeInterval <= 0 || c.InternalPort < 1 || c.InternalPort > 65535 {
		return errors.New("positive timeouts/probe interval and valid internal port are required")
	}
	return nil
}

// Executables are explicit core paths, never shell launch shortcuts.
func validateExecutable(path string) error {
	if path == "" || !filepath.IsAbs(path) {
		return errors.New("configure an absolute core executable path")
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".bat", ".cmd", ".command", ".ps1":
		return errors.New("shell launch shortcuts cannot be used as core executables")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("executable path is a directory")
	}
	return nil
}
