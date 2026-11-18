// SPDX-License-Identifier: Apache-2.0
package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

func mlxModelType(c Config, dir string) string {
	if c.Platform != "darwin" {
		return ""
	}
	hasMLX := validateExecutable(c.Binaries.MLXPython) == nil
	hasOMLX := validateExecutable(c.Binaries.OMLX) == nil
	if !hasMLX && !hasOMLX {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		return ""
	}
	var config struct {
		ModelType string `json:"model_type"`
		ModelFile string `json:"model_file"`
	}
	if json.Unmarshal(data, &config) != nil || config.ModelType == "" || !hasMLXWeights(dir) {
		return ""
	}
	if config.ModelFile != "" || config.ModelType == "plamo3" {
		if hasMLX && dir == filepath.Clean(c.PLaMoMLXDir) &&
			config.ModelType == "plamo3" && config.ModelFile == "modeling_plamo_mlx.py" &&
			hasPLaMoFiles(dir) {
			return "plamo-mlx"
		}
		return ""
	}
	name := strings.ToLower(filepath.Base(dir))
	if isFile(filepath.Join(dir, "oq_imatrix_report.json")) || strings.Contains(name, "-oq") {
		if hasOMLX {
			return "omlx"
		}
		return ""
	}
	if hasMLX {
		return "mlx"
	}
	return "omlx"
}

func hasPLaMoFiles(dir string) bool {
	for _, name := range []string{
		"serve.py", "server.py", "serving_memory.py", "modeling_plamo_mlx.py",
		"tokenization_plamo.py", "tokenizer.jsonl", "tokenizer_config.json", "chat_template.jinja",
	} {
		if !isFile(filepath.Join(dir, name)) {
			return false
		}
	}
	return true
}

func hasMLXWeights(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "model.safetensors.index.json"))
	if os.IsNotExist(err) {
		return isFile(filepath.Join(dir, "model.safetensors"))
	}
	if err != nil {
		return false
	}
	var index struct {
		WeightMap map[string]string `json:"weight_map"`
	}
	if json.Unmarshal(data, &index) != nil || len(index.WeightMap) == 0 {
		return false
	}
	shards := map[string]bool{}
	for _, name := range index.WeightMap {
		shards[name] = true
	}
	for name := range shards {
		if filepath.Base(name) != name || filepath.Ext(name) != ".safetensors" ||
			!isFile(filepath.Join(dir, name)) {
			return false
		}
	}
	return true
}

func setPLaMoTarget(payload map[string]any, target string) {
	lang := strings.ToLower(strings.TrimSpace(target))
	if lang == "" {
		return
	}
	kwargs, _ := payload["chat_template_kwargs"].(map[string]any)
	if kwargs == nil {
		kwargs = map[string]any{}
		payload["chat_template_kwargs"] = kwargs
	}
	if _, explicit := kwargs["target_lang"]; explicit {
		return
	}
	switch lang {
	case "traditional chinese", "zh-tw", "zh-hk", "zh-mo":
		lang = "zh-hant"
	case "simplified chinese", "zh-cn", "zh-sg":
		lang = "zh-hans"
	}
	kwargs["target_lang"] = lang
}

func adaptPLaMoPayload(payload map[string]any, target string) {
	if target != "" {
		setPLaMoTarget(payload, target)
		if messages, ok := payload["messages"].([]any); ok {
			translated := make([]any, 0, len(messages))
			for _, message := range messages {
				if value, ok := message.(map[string]any); !ok || value["role"] != "system" {
					translated = append(translated, message)
				}
			}
			payload["messages"] = translated
		}
	}
	if _, explicit := payload["stop"]; !explicit {
		payload["stop"] = []string{"<|plamo:tag|>", "<|plamo:bos|>"}
	}
}
