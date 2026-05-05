package disclosure

import (
	"strings"
	"testing"
)

func TestBudgetAwareEscalateGreen(t *testing.T) {
	d := &Disclosure{cfg: Config{Enabled: true, L1Tokens: 15, L2Tokens: 60}}
	content := strings.Repeat("Some content here. ", 100) // ~2000 chars
	result, saved := d.BudgetAwareEscalate(nil, content, 0)
	if saved != 0 {
		t.Error("green budget should not compress")
	}
	if result != content {
		t.Error("green budget should return content unchanged")
	}
}

func TestBudgetAwareEscalateYellow(t *testing.T) {
	d := &Disclosure{cfg: Config{Enabled: true, L1Tokens: 15, L2Tokens: 60}}
	content := strings.Repeat("Some content here with more detail. ", 100) // >2000 chars
	result, saved := d.BudgetAwareEscalate(nil, content, 1)
	if saved <= 0 {
		t.Error("yellow budget should compress large content")
	}
	if len(result) >= len(content) {
		t.Error("result should be shorter")
	}
}

func TestBudgetAwareEscalateYellowSmall(t *testing.T) {
	d := &Disclosure{cfg: Config{Enabled: true, L1Tokens: 15, L2Tokens: 60}}
	content := "Short content under 2000 chars."
	_, saved := d.BudgetAwareEscalate(nil, content, 1)
	if saved != 0 {
		t.Error("yellow should not compress small content")
	}
}

func TestBudgetAwareEscalateRed(t *testing.T) {
	d := &Disclosure{cfg: Config{Enabled: true, L1Tokens: 15, L2Tokens: 60}}
	content := strings.Repeat("Red budget content that should be heavily truncated. ", 50) // >1000 chars
	result, saved := d.BudgetAwareEscalate(nil, content, 2)
	if saved <= 0 {
		t.Error("red budget should compress large content")
	}
	if len(result) >= len(content) {
		t.Error("result should be shorter on red budget")
	}
}

func TestBudgetAwareEscalateRedMedium(t *testing.T) {
	d := &Disclosure{cfg: Config{Enabled: true, L1Tokens: 15, L2Tokens: 60}}
	content := strings.Repeat("Medium content. ", 40) // ~600 chars, between 500-1000
	result, saved := d.BudgetAwareEscalate(nil, content, 2)
	if saved <= 0 {
		t.Error("red budget should compress medium content (500-1000)")
	}
	if len(result) >= len(content) {
		t.Error("result should be shorter")
	}
}

func TestBudgetAwareEscalateRedSmall(t *testing.T) {
	d := &Disclosure{cfg: Config{Enabled: true, L1Tokens: 15, L2Tokens: 60}}
	content := "Short content."
	_, saved := d.BudgetAwareEscalate(nil, content, 2)
	if saved != 0 {
		t.Error("red should not compress very small content")
	}
}

func TestBudgetAwareEscalateEmpty(t *testing.T) {
	d := &Disclosure{cfg: Config{Enabled: true, L1Tokens: 15, L2Tokens: 60}}
	result, saved := d.BudgetAwareEscalate(nil, "", 2)
	if saved != 0 {
		t.Error("empty should not compress")
	}
	if result != "" {
		t.Error("empty should return empty")
	}
}

func TestBudgetAwareEscalateInvalid(t *testing.T) {
	d := &Disclosure{cfg: Config{Enabled: true, L1Tokens: 15, L2Tokens: 60}}
	content := strings.Repeat("Content. ", 200)
	_, saved := d.BudgetAwareEscalate(nil, content, 5) // invalid level
	if saved != 0 {
		t.Error("invalid budget level should not compress")
	}
}

