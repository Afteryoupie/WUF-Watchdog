// SPDX-License-Identifier: Apache-2.0
package core

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOptionalMLXDiscovery(t *testing.T) {
	c := testConfig(t)
	c.Platform = "darwin"
	c.Binaries.MLXPython = writeFile(t, filepath.Join(t.TempDir(), "python3"))
	c.Binaries.OMLX = writeFile(t, filepath.Join(t.TempDir(), "omlx"))
	c.PLaMoMLXDir = filepath.Join(c.ModelsDir, "plamo-translate")
	for _, name := range []string{"regular", "optimized-OQ", "plamo-translate", "other-plamo", "partial"} {
		dir := filepath.Join(c.ModelsDir, name)
		config := writeFile(t, filepath.Join(dir, "config.json"))
		if err := os.WriteFile(config, []byte(`{"model_type":"llama","quantization":{"bits":4}}`), 0600); err != nil {
			t.Fatal(err)
		}
		if name != "partial" {
			writeFile(t, filepath.Join(dir, "model.safetensors"))
		}
	}
	for _, dir := range []string{c.PLaMoMLXDir, filepath.Join(c.ModelsDir, "other-plamo")} {
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"model_type":"plamo3","model_file":"modeling_plamo_mlx.py"}`), 0600); err != nil {
			t.Fatal(err)
		}
		for _, file := range []string{"serve.py", "server.py", "serving_memory.py", "modeling_plamo_mlx.py", "tokenization_plamo.py", "tokenizer.jsonl", "tokenizer_config.json", "chat_template.jinja"} {
			writeFile(t, filepath.Join(dir, file))
		}
	}
	partial := filepath.Join(c.ModelsDir, "partial")
	index := writeFile(t, filepath.Join(partial, "model.safetensors.index.json"))
	if err := os.WriteFile(index, []byte(`{"weight_map":{"a":"model-00001-of-00002.safetensors","b":"model-00002-of-00002.safetensors"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(partial, "model-00001-of-00002.safetensors"))
	assertModels := func(want map[string]string) {
		t.Helper()
		models, err := Discover(c)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, model := range models {
			if model.Type != "gguf" && model.Type != "apple" && model.Type != "apple-api" {
				got[model.ID] = model.Type
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("model types got %v, want %v", got, want)
		}
	}
	assertModels(map[string]string{"regular": "mlx", "optimized-OQ": "omlx", "plamo-translate": "plamo-mlx"})
	writeFile(t, filepath.Join(partial, "model-00002-of-00002.safetensors"))
	assertModels(map[string]string{"regular": "mlx", "optimized-OQ": "omlx", "plamo-translate": "plamo-mlx", "partial": "mlx"})
	c.Platform = "windows"
	assertModels(map[string]string{})
}

func TestMLXBackendCommandAndPayload(t *testing.T) {
	c := testConfig(t)
	c.Platform = "darwin"
	c.Binaries.MLXPython = writeFile(t, filepath.Join(t.TempDir(), "python3"))
	c.Binaries.OMLX = writeFile(t, filepath.Join(t.TempDir(), "omlx"))
	c.PLaMoMLXDir = filepath.Join(c.ModelsDir, "pfnet")
	serve := writeFile(t, filepath.Join(c.PLaMoMLXDir, "serve.py"))
	for _, tc := range []struct {
		kind, health string
		argv         []string
	}{
		{"mlx", "/v1/models", []string{c.Binaries.MLXPython, "-m", "mlx_lm.server", "--model", c.PLaMoMLXDir, "--host", "127.0.0.1", "--port", "8081"}},
		{"omlx", "/health", []string{c.Binaries.OMLX, "serve", "--model-dir", c.PLaMoMLXDir, "--host", "127.0.0.1", "--port", "8081", "--log-level", "warning", "--base-path", filepath.Join(c.DataDir, "omlx"), "--no-hf-cache"}},
		{"plamo-mlx", "/v1/chat/completions", []string{c.Binaries.MLXPython, "-B", serve, "--model", c.PLaMoMLXDir, "--host", "127.0.0.1", "--port", "8081", "--trust-remote-code"}},
	} {
		model := Model{ID: "pfnet", Path: c.PLaMoMLXDir, Type: tc.kind}
		argv, err := Command(c, model)
		if err != nil || !reflect.DeepEqual(argv, tc.argv) || HealthPath(model) != tc.health {
			t.Errorf("%s command %v, health %s, err %v", tc.kind, argv, HealthPath(model), err)
		}
	}
	model := Model{ID: "plamo-translate", Path: c.PLaMoMLXDir, Type: "plamo-mlx"}
	user := map[string]any{"role": "user", "content": "Hello."}
	payload := map[string]any{
		"target_language":      "Traditional Chinese",
		"messages":             []any{map[string]any{"role": "system", "content": "Translate into Chinese."}, user},
		"chat_template_kwargs": map[string]any{"source_lang": "en"},
	}
	AdaptPayload(payload, model)
	want := map[string]any{
		"model": model.Path, "messages": []any{user},
		"chat_template_kwargs": map[string]any{"source_lang": "en", "target_lang": "zh-hant"},
		"stop":                 []string{"<|plamo:tag|>", "<|plamo:bos|>"},
	}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("PLaMo payload: %#v", payload)
	}
	model.Path = filepath.Join(c.ModelsDir, "unconfigured")
	if _, err := Command(c, model); err == nil {
		t.Fatal("custom code must require an explicit model directory")
	}
}
