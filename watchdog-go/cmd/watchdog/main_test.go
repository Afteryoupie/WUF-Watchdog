// SPDX-License-Identifier: Apache-2.0
package main

import (
	"reflect"
	"testing"
)

func TestStripLegacyFlags(t *testing.T) {
	in := []string{
		"--listen", "127.0.0.1:8080",
		"--laya-root", "/tmp/laya",
		"--laya-python=/usr/bin/python3",
		"--laya-model", "/tmp/laya-model",
		"--python", "/usr/bin/python3",
		"--rapid-mlx=/usr/local/bin/rapid-mlx",
		"--omlx", "/usr/local/bin/omlx",
		"--decision-port=8082",
		"--decision-port", "8083",
		"--dashboard=/tmp/dashboard.html",
		"--models-dir", "/tmp/models",
	}
	got := stripLegacyFlags(in)
	want := []string{
		"--listen", "127.0.0.1:8080",
		"--dashboard=/tmp/dashboard.html",
		"--models-dir", "/tmp/models",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stripLegacyFlags() = %v, want %v", got, want)
	}
}
