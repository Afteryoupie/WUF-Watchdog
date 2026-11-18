// SPDX-License-Identifier: Apache-2.0
package core

import (
	"reflect"
	"testing"
)

func TestAdaptPLaMoGGUFTarget(t *testing.T) {
	model := Model{ID: "plamo-3-translate-2b-Q4_K_M", Type: "gguf", Path: "/models/plamo.gguf"}
	messages := []any{
		map[string]any{"role": "system", "content": "Translate into Traditional Chinese."},
		map[string]any{"role": "user", "content": "I'll be right back."},
	}
	for _, tc := range []struct {
		name, target, explicit, want string
	}{
		{"Traditional Chinese", "Traditional Chinese", "", "zh-hant"},
		{"French", "fr", "", "fr"},
		{"explicit template language", "Traditional Chinese", "ja", "ja"},
		{"automatic direction", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kwargs := map[string]any{"context": "Dialogue on a train."}
			if tc.explicit != "" {
				kwargs["target_lang"] = tc.explicit
			}
			payload := map[string]any{
				"target_language": tc.target, "messages": messages, "chat_template_kwargs": kwargs,
			}
			AdaptPayload(payload, model)

			wantKwargs := map[string]any{"context": "Dialogue on a train."}
			if tc.want != "" {
				wantKwargs["target_lang"] = tc.want
			}
			want := map[string]any{"model": model.Path, "messages": messages, "chat_template_kwargs": wantKwargs}
			if !reflect.DeepEqual(payload, want) {
				t.Fatalf("payload = %#v, want %#v", payload, want)
			}
		})
	}
}
