package handler

import "testing"

func TestOptimizerAllowed(t *testing.T) {
	dummy := struct{}{}

	tests := []struct {
		name      string
		overrides map[string]bool
		stage     string
		instance  interface{}
		want      bool
	}{
		{"nil overrides, instance present", nil, "chunker", dummy, true},
		{"nil overrides, nil instance", nil, "chunker", nil, false},
		{"empty overrides, instance present", map[string]bool{}, "chunker", dummy, true},
		{"empty overrides, nil instance", map[string]bool{}, "chunker", nil, false},
		{"override true, instance present", map[string]bool{"chunker": true}, "chunker", dummy, true},
		{"override true, nil instance", map[string]bool{"chunker": true}, "chunker", nil, false},
		{"override false, instance present", map[string]bool{"chunker": false}, "chunker", dummy, false},
		{"override false, nil instance", map[string]bool{"chunker": false}, "chunker", nil, false},
		{"unrelated override, instance present", map[string]bool{"other": true}, "chunker", dummy, true},
		{"unrelated override, nil instance", map[string]bool{"other": true}, "chunker", nil, false},
		{"multiple overrides, target present", map[string]bool{"chunker": true, "textcomp": false}, "textcomp", dummy, false},
		{"multiple overrides, target absent", map[string]bool{"chunker": true, "textcomp": false}, "pordee", dummy, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := optimizerAllowed(tt.overrides, tt.stage, tt.instance)
			if got != tt.want {
				t.Errorf("optimizerAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOptimizerAllowed_SemanticDedupSentinel(t *testing.T) {
	// semantic_dedup passes `true` as instance since tokenizer is always available
	if !optimizerAllowed(nil, "semantic_dedup", true) {
		t.Error("semantic_dedup should be allowed by default (instance=true)")
	}
	if optimizerAllowed(map[string]bool{"semantic_dedup": false}, "semantic_dedup", true) {
		t.Error("semantic_dedup override false should block even with instance=true")
	}
	if !optimizerAllowed(map[string]bool{"semantic_dedup": true}, "semantic_dedup", true) {
		t.Error("semantic_dedup override true with instance=true should allow")
	}
}
