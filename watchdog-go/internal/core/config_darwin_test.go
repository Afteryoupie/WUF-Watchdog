// SPDX-License-Identifier: Apache-2.0

package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigDiscoversAppleTranslationBackend(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WUF_APPLE_API", "")
	path := filepath.Join(home, "LLM", "Tools", "apple-intelligence-api", "AppleIntelligenceApi")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0755); err != nil {
		t.Fatal(err)
	}
	config, err := DefaultConfig()
	if err != nil || config.Binaries.AppleAPI != path {
		t.Fatalf("Apple backend = %q, err = %v, want %q", config.Binaries.AppleAPI, err, path)
	}
	override := filepath.Join(home, "custom-apple-backend")
	t.Setenv("WUF_APPLE_API", override)
	config, err = DefaultConfig()
	if err != nil || config.Binaries.AppleAPI != override {
		t.Fatalf("Apple backend = %q, err = %v, want %q", config.Binaries.AppleAPI, err, override)
	}
}
