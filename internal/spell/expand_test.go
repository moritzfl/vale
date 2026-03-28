package spell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandGerman(t *testing.T) {
	affPath := "../../testdata/styles/config/dictionaries/de_DE.aff"
	dicPath := "../../testdata/styles/config/dictionaries/de_DE.dic"

	gs, err := newGoSpell(affPath, dicPath)
	if err != nil {
		t.Fatalf("failed to load German dictionary: %v", err)
	}

	tests := []struct {
		word     string
		expected int
	}{
		{"gut", 6},
		{"gute", 6},
		{"guten", 6},
		{"gutem", 6},
		{"guter", 6},
		{"geh", 2},
		{"gehen", 2},
		{"unknown", 1}, // No expansion
	}

	for _, test := range tests {
		forms := gs.Expand(test.word)
		if len(forms) != test.expected {
			t.Errorf("Expand(%q) returned %d forms, expected %d: %v",
				test.word, len(forms), test.expected, forms)
		}
	}

	// Test that "gut" is in the expanded forms of "gut"
	gutForms := gs.Expand("gut")
	found := false
	for _, form := range gutForms {
		if form == "gut" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected 'gut' to be in its own expanded forms: %v", gutForms)
	}
}

func TestExpandGermanChecker(t *testing.T) {
	affPath := "../../testdata/styles/config/dictionaries/de_DE.aff"
	dicPath := "../../testdata/styles/config/dictionaries/de_DE.dic"

	checker, err := NewChecker(UsingDictionaryByPath(dicPath, affPath))
	if err != nil {
		t.Fatalf("failed to create checker: %v", err)
	}

	forms := checker.Expand("gut")
	if len(forms) != 6 {
		t.Errorf("Expand(gut) returned %d forms, expected 6: %v", len(forms), forms)
	}

	// Test word not in dictionary
	forms = checker.Expand("xyz")
	if len(forms) != 1 || forms[0] != "xyz" {
		t.Errorf("Expand(xyz) returned %v, expected [xyz]", forms)
	}
}

func TestExpandGermanFileNotFound(t *testing.T) {
	_, err := newGoSpell("/nonexistent/de_DE.aff", "/nonexistent/de_DE.dic")
	if err == nil {
		t.Error("Expected error for nonexistent files")
	}
}

func TestExpandLoadFailureMentionsUTF8Encoding(t *testing.T) {
	dir := t.TempDir()

	affPath := filepath.Join(dir, "invalid.aff")
	dicPath := filepath.Join(dir, "invalid.dic")

	if err := os.WriteFile(affPath, []byte("TRY\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dicPath, []byte("1\nfoo\n"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := newGoSpell(affPath, dicPath)
	if err == nil {
		t.Fatal("expected invalid dictionary to return an error")
	}
	if !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("expected UTF-8 hint in error, got %v", err)
	}
}

func TestExpandMergesFormsAcrossDictionaries(t *testing.T) {
	dir := t.TempDir()

	affA := filepath.Join(dir, "de_DE.aff")
	dicA := filepath.Join(dir, "de_DE.dic")
	affB := filepath.Join(dir, "de_AT.aff")
	dicB := filepath.Join(dir, "de_AT.dic")

	if err := os.WriteFile(affA, []byte("SET ISO8859-1\nSFX A Y 1\nSFX A 0 e .\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dicA, []byte("1\ngut/A\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(affB, []byte("SET ISO8859-1\nSFX B Y 1\nSFX B 0 er .\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dicB, []byte("1\ngut/B\n"), 0600); err != nil {
		t.Fatal(err)
	}

	checker, err := NewChecker(
		UsingDictionaryByPath(dicA, affA),
		UsingDictionaryByPath(dicB, affB),
	)
	if err != nil {
		t.Fatalf("failed to create checker: %v", err)
	}

	forms := checker.Expand("gut")
	expected := map[string]struct{}{"gut": {}, "gute": {}, "guter": {}}
	if len(forms) != len(expected) {
		t.Fatalf("expected %d forms, got %d: %v", len(expected), len(forms), forms)
	}
	for _, form := range forms {
		if _, ok := expected[form]; !ok {
			t.Fatalf("unexpected form %q in %v", form, forms)
		}
	}
}

func TestExpandWithLineageKeepsAffixDerivations(t *testing.T) {
	dir := t.TempDir()

	affPath := filepath.Join(dir, "custom.aff")
	dicPath := filepath.Join(dir, "custom.dic")

	if err := os.WriteFile(affPath, []byte("SET ISO8859-1\nSFX D Y 1\nSFX D e ed e\nSFX G Y 1\nSFX G e ing e\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dicPath, []byte("2\noptimize/DG\nstreamline/GD\n"), 0600); err != nil {
		t.Fatal(err)
	}

	checker, err := NewChecker(UsingDictionaryByPath(dicPath, affPath))
	if err != nil {
		t.Fatalf("failed to create checker: %v", err)
	}

	optimize := checker.ExpandWithLineage("optimize")
	if len(optimize) != 3 {
		t.Fatalf("expected 3 optimize inflections, got %d: %#v", len(optimize), optimize)
	}

	optimizeByLineage := map[string]string{}
	for _, inflection := range optimize {
		optimizeByLineage[inflection.Lineage] = inflection.Form
	}
	if optimizeByLineage["S:D:0"] != "optimized" {
		t.Fatalf("expected lineage S:D:0 to map to optimized, got %q", optimizeByLineage["S:D:0"])
	}
	if optimizeByLineage["S:G:0"] != "optimizing" {
		t.Fatalf("expected lineage S:G:0 to map to optimizing, got %q", optimizeByLineage["S:G:0"])
	}

	streamline := checker.ExpandWithLineage("streamline")
	streamlineByLineage := map[string]string{}
	for _, inflection := range streamline {
		streamlineByLineage[inflection.Lineage] = inflection.Form
	}
	if streamlineByLineage["S:D:0"] != "streamlined" {
		t.Fatalf("expected lineage S:D:0 to map to streamlined, got %q", streamlineByLineage["S:D:0"])
	}
	if streamlineByLineage["S:G:0"] != "streamlining" {
		t.Fatalf("expected lineage S:G:0 to map to streamlining, got %q", streamlineByLineage["S:G:0"])
	}
}
