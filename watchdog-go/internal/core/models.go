// SPDX-License-Identifier: Apache-2.0
package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Model struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	Type          string `json:"type"`
	IsTranslation bool   `json:"is_translation"`
	IdleTimeout   int    `json:"idle_timeout"`
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func isTranslation(name string) bool {
	name = strings.ToLower(name)
	for _, marker := range []string{"qwen3vl", "mt", "translat"} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

func modelPriority(id string) int {
	id = strings.ToLower(id)
	switch {
	case strings.Contains(id, "qwen3vl"):
		return 0
	case strings.Contains(id, "hy-mt2") || strings.Contains(id, "translate"):
		return 1
	case strings.Contains(id, "ling-3.0-tiny-mlx"):
		return 2
	case strings.Contains(id, "ling"):
		return 3
	default:
		return 4
	}
}

func Discover(c Config) ([]Model, error) {
	models := []Model{}
	add := func(id, path, kind string, translation bool) {
		models = append(models, Model{id, id, path, kind, translation, int(c.IdleTimeout.Seconds())})
	}
	if c.Platform == "darwin" {
		if isFile(c.Binaries.AppleAPI) {
			add("apple-intelligence-translation", "permissive", "apple-api", true)
		} else if isFile(c.Binaries.FM) {
			add("apple-intelligence-translation", "system", "apple", false)
		}
		if len(models) > 0 {
			models[0].Name = "Apple Foundation Models"
		}
	}
	entries, err := os.ReadDir(c.ModelsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return models, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.Contains(strings.ToLower(name), "mmproj") {
			continue
		}
		path, err := filepath.Abs(filepath.Join(c.ModelsDir, name))
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			if isFile(filepath.Join(path, "rl_agent_config.json")) {
				continue
			}
			children, err := os.ReadDir(path)
			if err != nil {
				return nil, err
			}
			weight := ""
			for _, child := range children {
				if !child.IsDir() && strings.EqualFold(filepath.Ext(child.Name()), ".gguf") &&
					!strings.Contains(strings.ToLower(child.Name()), "mmproj") {
					weight = filepath.Join(path, child.Name())
					break
				}
			}
			switch {
			case weight != "":
				add(name, weight, "gguf", isTranslation(name))
			case c.Platform == "windows":
				continue
			case isFile(filepath.Join(path, "oq_imatrix_report.json")) || strings.Contains(strings.ToLower(name), "oq"):
				add(name, path, "omlx", isTranslation(name))
			case isFile(filepath.Join(path, "config.json")):
				add(name, path, "mlx", isTranslation(name))
			}
		} else if info.Mode().IsRegular() && strings.EqualFold(filepath.Ext(name), ".gguf") {
			id := strings.TrimSuffix(name, filepath.Ext(name))
			add(id, path, "gguf", isTranslation(id))
		}
	}
	sort.SliceStable(models, func(i, j int) bool { return modelPriority(models[i].ID) < modelPriority(models[j].ID) })
	return models, nil
}

func FindModel(models []Model, alias string) (Model, bool) {
	for _, model := range models {
		if alias == model.ID || alias == model.Name || alias == model.Path {
			return model, true
		}
	}
	return Model{}, false
}

func LoadLastModel(dataDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dataDir, "last_model.json"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var saved struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		return "", err
	}
	return strings.TrimSpace(saved.ID), nil
}

func SaveLastModel(dataDir, id string) error {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(struct {
		ID string `json:"id"`
	}{id})
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dataDir, ".last-model-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(dataDir, "last_model.json"))
}
