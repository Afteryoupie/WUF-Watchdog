// SPDX-License-Identifier: Apache-2.0
package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func Projector(c Config, model Model) string {
	lower := strings.ToLower(model.Name)
	vision := false
	for _, marker := range []string{"vl", "vision", "llava", "minicpm", "moondream", "pixtral", "ovis"} {
		vision = vision || strings.Contains(lower, marker)
	}
	if !vision {
		return ""
	}
	// A nested model directory identifies its projector. For loose weights,
	// require the model ID in the projector name instead of taking any mmproj.
	entries, err := os.ReadDir(filepath.Dir(model.Path))
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if filepath.Clean(filepath.Dir(model.Path)) == filepath.Clean(c.ModelsDir) &&
			!strings.Contains(name, strings.ToLower(model.ID)) {
			continue
		}
		if !entry.IsDir() && strings.Contains(name, "mmproj") && strings.HasSuffix(name, ".gguf") {
			return filepath.Join(filepath.Dir(model.Path), entry.Name())
		}
	}
	return ""
}

func ContextSize(c Config, model Model) int {
	var modelMiB int64 = 6000
	if info, err := os.Stat(model.Path); err == nil && !info.IsDir() {
		modelMiB = info.Size() / (1024 * 1024)
	}
	if info, err := os.Stat(Projector(c, model)); err == nil {
		modelMiB += info.Size() / (1024 * 1024)
	}
	remaining := max(int64(1000), c.VRAMMiB-1536) - modelMiB
	lower := strings.ToLower(model.Name)
	for _, marker := range []string{"27b", "30b", "32b", "34b", "70b"} {
		if strings.Contains(lower, marker) {
			if remaining >= 2500 {
				return 16384
			}
			return 8192
		}
	}
	if remaining >= 3000 {
		return 32768
	}
	if remaining >= 1400 {
		return 16384
	}
	return 8192
}

// Command returns argv directly; no shell parsing, .bat or .command involved.
func Command(c Config, model Model) ([]string, error) {
	port := strconv.Itoa(c.InternalPort)
	ctx := strconv.Itoa(ContextSize(c, model))
	var argv []string
	switch model.Type {
	case "gguf":
		k2 := strings.Contains(strings.ToLower(model.ID+" "+model.Name+" "+model.Path), "k2-horizon")
		if k2 {
			argv = []string{c.Binaries.K2Llama, "-m", model.Path, "--host", "127.0.0.1", "--port", port,
				"-c", "16384", "-np", "1", "-ngl", "99", "--jinja", "--no-webui"}
		} else {
			argv = []string{c.Binaries.Llama, "--model", model.Path, "--host", "127.0.0.1", "--port", port,
				"--ctx-size", ctx, "--n-gpu-layers", "99", "--batch-size", "512", "--ubatch-size", "512",
				"--flash-attn", "on", "--cache-type-k", "q8_0", "--cache-type-v", "q8_0"}
			if projector := Projector(c, model); projector != "" {
				argv = append(argv, "--mmproj", projector)
			}
		}
	case "apple-api":
		argv = []string{c.Binaries.AppleAPI, "serve", "--hostname", "127.0.0.1", "--port", port}
	case "apple":
		argv = []string{c.Binaries.FM, "serve", "--host", "127.0.0.1", "--port", port}
	case "omlx":
		argv = []string{c.Binaries.OMLX, "serve", "--model-dir", c.ModelsDir, "--host", "127.0.0.1", "--port", port, "--log-level", "warning"}
	case "mlx":
		if isFile(c.Binaries.Rapid) {
			argv = []string{c.Binaries.Rapid, "serve", model.Path, "--host", "127.0.0.1", "--port", port,
				"--max-tokens", ctx, "--log-level", "WARNING", "--watchdog-ppid", strconv.Itoa(os.Getpid())}
		} else {
			argv = []string{c.Binaries.Python, "-m", "mlx_lm.server", "--model", model.Path,
				"--host", "127.0.0.1", "--port", port, "--max-tokens", ctx}
		}
	default:
		return nil, fmt.Errorf("unsupported backend type %q", model.Type)
	}
	if model.Type != "gguf" && c.Platform != "darwin" {
		return nil, fmt.Errorf("backend %s requires macOS", model.Type)
	}
	if err := validateExecutable(argv[0]); err != nil {
		return nil, fmt.Errorf("%s executable: %w", model.Type, err)
	}
	return argv, nil
}

func HealthPath(model Model) string {
	switch model.Type {
	case "apple":
		return "/v1/models"
	case "apple-api":
		return "/api/v1/models"
	default:
		return "/health"
	}
}
