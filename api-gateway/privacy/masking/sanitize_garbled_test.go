package masking

import "testing"

// 2+ consecutive undef tokens are model garble for ANY provider and must be
// stripped; a single standalone token can be legitimate code and is kept.
func TestSanitizeGarbledForModel_AllModels(t *testing.T) {
	u := "un" + "defined"
	for _, model := range []string{"claude-sonnet-4-6", "glm-4.6", "claude-opus-4-7", ""} {
		if got := SanitizeGarbledForModel(model, u+u+"X"); got != "X" {
			t.Errorf("[%s] doubled: got %q want X", model, got)
		}
		if got := SanitizeGarbledForModel(model, u+u+u+"Y"); got != "Y" {
			t.Errorf("[%s] triple: got %q want Y", model, got)
		}
		if got := SanitizeGarbledForModel(model, "a"+u+u+"b"); got != "ab" {
			t.Errorf("[%s] embedded doubled: got %q want ab", model, got)
		}
		legit := "typeof x === " + u
		if got := SanitizeGarbledForModel(model, legit); got != legit {
			t.Errorf("[%s] single must be preserved: got %q", model, got)
		}
	}
}
