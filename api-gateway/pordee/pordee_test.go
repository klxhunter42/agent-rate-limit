package pordee

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func newTestPipeline() *Pipeline {
	reg := prometheus.NewRegistry()
	return New(reg)
}

func TestHasThai(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"สวัสดีครับ", true},
		{"ตรวจสอบ pod ที่ pending", true},
		{"Hello World", false},
		{"kubectl get pods", false},
		{"พอดี mode active", true},
		{"", false},
		{"mix ภาษาไทย and English", true},
		{"12345 !@#$%", false},
		{"ครับค่ะนะคะ", true},
	}

	for _, tt := range tests {
		got := HasThai(tt.input)
		if got != tt.want {
			t.Errorf("HasThai(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestShouldInject_Disabled(t *testing.T) {
	p := newTestPipeline()
	p.cfg.Enabled = false

	should, level := p.ShouldInject("สวัสดีครับ", 0)
	if should {
		t.Error("ShouldInject should return false when disabled")
	}
	if level != Off {
		t.Errorf("level = %v, want Off", level)
	}
}

func TestShouldInject_NoThai(t *testing.T) {
	p := newTestPipeline()

	should, level := p.ShouldInject("Hello World", 0)
	if should {
		t.Error("ShouldInject should return false when no Thai")
	}
	if level != Off {
		t.Errorf("level = %v, want Off", level)
	}
}

func TestShouldInject_ThaiLite(t *testing.T) {
	p := newTestPipeline()
	p.cfg.Level = Lite

	should, level := p.ShouldInject("ตรวจสอบ pod", 0)
	if !should {
		t.Error("ShouldInject should return true for Thai text")
	}
	if level != Lite {
		t.Errorf("level = %v, want Lite", level)
	}
}

func TestShouldInject_ThaiFull(t *testing.T) {
	p := newTestPipeline()
	p.cfg.Level = Full

	should, level := p.ShouldInject("แก้ไข terraform", 0)
	if !should {
		t.Error("ShouldInject should return true for Thai text")
	}
	if level != Full {
		t.Errorf("level = %v, want Full", level)
	}
}

func TestShouldInject_BudgetRed(t *testing.T) {
	p := newTestPipeline()
	p.cfg.Level = Lite

	should, level := p.ShouldInject("ตรวจสอบ pod", 2)
	if !should {
		t.Error("ShouldInject should return true for Thai text")
	}
	if level != Full {
		t.Errorf("budget red should force Full, got %v", level)
	}
}

func TestInject_Lite(t *testing.T) {
	p := newTestPipeline()
	sys := "You are a helpful assistant."

	result, ratio := p.Inject(sys, Lite)
	if !strings.Contains(result, sys) {
		t.Error("result should contain original system prompt")
	}
	if !strings.Contains(result, "PORDEE MODE - lite") {
		t.Error("result should contain lite injection")
	}
	if ratio <= 0 || ratio >= 1 {
		t.Errorf("ratio = %v, want (0, 1)", ratio)
	}
}

func TestInject_Full(t *testing.T) {
	p := newTestPipeline()
	sys := "You are a helpful assistant."

	result, ratio := p.Inject(sys, Full)
	if !strings.Contains(result, "PORDEE MODE - full") {
		t.Error("result should contain full injection")
	}
	if !strings.Contains(result, "Auto-clarity") {
		t.Error("full injection should include auto-clarity rules")
	}
	if ratio >= 0.5 {
		t.Errorf("full ratio = %v, expect aggressive (< 0.5)", ratio)
	}
}

func TestInject_PreservesOriginal(t *testing.T) {
	p := newTestPipeline()
	sys := "Important instruction: do X."

	result, _ := p.Inject(sys, Full)
	if !strings.HasPrefix(result, sys) {
		t.Error("injection should append, not prepend")
	}
}

func TestMetricsRecorded(t *testing.T) {
	reg := prometheus.NewRegistry()
	p := New(reg)

	p.ShouldInject("สวัสดี", 0)

	count := testutil.ToFloat64(p.m.injections.WithLabelValues("full", "valid"))
	if count != 1 {
		t.Errorf("expected 1 valid injection, got %v", count)
	}

	p.ShouldInject("Hello", 0)
	noThai := testutil.ToFloat64(p.m.injections.WithLabelValues("off", "no_thai"))
	if noThai != 1 {
		t.Errorf("expected 1 no_thai count, got %v", noThai)
	}
}

func TestLevelString(t *testing.T) {
	tests := []struct {
		level Level
		want  string
	}{
		{Off, "off"},
		{Lite, "lite"},
		{Full, "full"},
	}
	for _, tt := range tests {
		if tt.level.String() != tt.want {
			t.Errorf("Level(%d).String() = %q, want %q", tt.level, tt.level.String(), tt.want)
		}
	}
}

func TestInject_EmptyPrompt(t *testing.T) {
	p := newTestPipeline()
	result, _ := p.Inject("", Full)
	if !strings.Contains(result, "PORDEE MODE") {
		t.Error("should inject even on empty prompt")
	}
}
