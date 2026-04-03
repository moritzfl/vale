package check

import (
	"testing"

	"github.com/errata-ai/vale/v3/internal/core"
)

func TestNewSpellingSkipsMorphologyIndexesByDefault(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictDir := t.TempDir()
	writeMorphDict(
		t,
		dictDir,
		"en_US",
		"SET UTF-8\nSFX A Y 2\nSFX A 0 d e\nSFX A e ing e\n",
		"1\nutilize/A\n",
	)

	rule, err := NewSpelling(cfg, baseCheck{
		"name":         "Custom.Spelling",
		"dictionaries": []string{"en_US"},
		"dicpath":      dictDir,
	}, "")
	if err != nil {
		t.Fatalf("failed to create spelling rule: %v", err)
	}

	if !rule.gs.Spell("utilized") {
		t.Fatal("expected inflected form to be recognized")
	}

	forms := rule.gs.Expand("utilize")
	if len(forms) != 1 || forms[0] != "utilize" {
		t.Fatalf("expected fallback expansion without morphology indexes, got %v", forms)
	}
}

func TestMakeSpellerCanKeepMorphologyIndexesWhenNeeded(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictDir := t.TempDir()
	writeMorphDict(
		t,
		dictDir,
		"en_US",
		"SET UTF-8\nSFX A Y 2\nSFX A 0 d e\nSFX A e ing e\n",
		"1\nutilize/A\n",
	)

	checker, err := makeSpeller(&Spelling{
		Dictionaries: []string{"en_US"},
		Dicpath:      dictDir,
	}, cfg, "")
	if err != nil {
		t.Fatalf("failed to create checker: %v", err)
	}

	forms := checker.Expand("utilize")
	if len(forms) <= 1 {
		t.Fatalf("expected morphology expansion with indexes enabled, got %v", forms)
	}
}
