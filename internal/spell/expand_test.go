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

func TestExpandSupportsUTF8NonASCIIFLags(t *testing.T) {
	checker := newCheckerFromInlineDict(t,
		"SET UTF-8\nSFX Ü Y 1\nSFX Ü 0 en .\n",
		"1\ngut/Ü\n",
	)

	assertFormsEqual(t, checker.Expand("gut"), map[string]struct{}{
		"gut":   {},
		"guten": {},
	})
}

func TestExpandSupportsUTF8EmojiFlags(t *testing.T) {
	checker := newCheckerFromInlineDict(t,
		"SET UTF-8\nFLAG UTF-8\nPFX ☎️ Y 1\nPFX ☎️ 0 tele .\nSFX S Y 1\nSFX S 0 s .\n",
		"1\nbanco/S☎️\n",
	)

	assertFormsEqual(t, checker.Expand("banco"), map[string]struct{}{
		"banco":      {},
		"bancos":     {},
		"telebanco":  {},
		"telebancos": {},
	})
}

func TestExpandSupportsLongFlags(t *testing.T) {
	checker := newCheckerFromInlineDict(t,
		"SET UTF-8\nFLAG long\nSFX AB Y 1\nSFX AB 0 en .\n",
		"1\ngut/AB\n",
	)

	assertFormsEqual(t, checker.Expand("gut"), map[string]struct{}{
		"gut":   {},
		"guten": {},
	})
}

func TestExpandLongFlagsDoNotCollide(t *testing.T) {
	checker := newCheckerFromInlineDict(t,
		"SET UTF-8\nFLAG long\nSFX AB Y 1\nSFX AB 0 en .\nSFX AC Y 1\nSFX AC 0 er .\n",
		"1\ngut/AB\n",
	)

	assertFormsEqual(t, checker.Expand("gut"), map[string]struct{}{
		"gut":   {},
		"guten": {},
	})
}

func TestExpandSupportsNumFlags(t *testing.T) {
	checker := newCheckerFromInlineDict(t,
		"SET UTF-8\nFLAG num\nSFX 12 Y 1\nSFX 12 0 en .\n",
		"1\ngut/12\n",
	)

	assertFormsEqual(t, checker.Expand("gut"), map[string]struct{}{
		"gut":   {},
		"guten": {},
	})
}

func TestExpandNumFlagsDoNotCollide(t *testing.T) {
	checker := newCheckerFromInlineDict(t,
		"SET UTF-8\nFLAG num\nSFX 1 Y 1\nSFX 1 0 en .\nSFX 12 Y 1\nSFX 12 0 er .\n",
		"1\ngut/12\n",
	)

	assertFormsEqual(t, checker.Expand("gut"), map[string]struct{}{
		"gut":   {},
		"guter": {},
	})
}

func TestExpandSupportsFlagAliases(t *testing.T) {
	checker := newCheckerFromInlineDict(t,
		"SET UTF-8\nAF 1\nAF A\nSFX A Y 1\nSFX A 0 en .\n",
		"1\ngut/1\n",
	)

	assertFormsEqual(t, checker.Expand("gut"), map[string]struct{}{
		"gut":   {},
		"guten": {},
	})
}

func TestExpandSupportsContinuationClasses(t *testing.T) {
	checker := newCheckerFromInlineDict(t,
		"SET UTF-8\nSFX A Y 1\nSFX A 0 able/B .\nSFX B Y 1\nSFX B 0 ness .\n",
		"1\naccept/A\n",
	)

	assertFormsEqual(t, checker.Expand("accept"), map[string]struct{}{
		"accept":         {},
		"acceptable":     {},
		"acceptableness": {},
	})
}

func TestExpandHonorsPrefixStripBehavior(t *testing.T) {
	checker := newCheckerFromInlineDict(t,
		"SET UTF-8\nPFX I Y 1\nPFX I p imp p\n",
		"1\npossible/I\n",
	)

	assertFormsEqual(t, checker.Expand("possible"), map[string]struct{}{
		"possible":   {},
		"impossible": {},
	})
}

func TestSpellSupportsLongFlagCompoundRules(t *testing.T) {
	checker := newCheckerFromInlineDict(t,
		"SET UTF-8\nFLAG long\nCOMPOUNDRULE 1\nCOMPOUNDRULE ABAC\n",
		"2\nnews/AB\npaper/AC\n",
	)

	if !checker.Spell("newspaper") {
		t.Fatal("expected compound word newspaper to be recognized")
	}
}

func TestSpellSupportsNumFlagCompoundRules(t *testing.T) {
	checker := newCheckerFromInlineDict(t,
		"SET UTF-8\nFLAG num\nCOMPOUNDRULE 1\nCOMPOUNDRULE 12,34\n",
		"2\nhaus/12\nboot/34\n",
	)

	if !checker.Spell("hausboot") {
		t.Fatal("expected compound word hausboot to be recognized")
	}
}

func TestSpellSupportsUTF8EmojiCompoundRules(t *testing.T) {
	checker := newCheckerFromInlineDict(t,
		"SET UTF-8\nFLAG UTF-8\nCOMPOUNDRULE 1\nCOMPOUNDRULE ☎️S\n",
		"2\ntele/☎️\nbanco/S\n",
	)

	if !checker.Spell("telebanco") {
		t.Fatal("expected compound word telebanco to be recognized")
	}
}

func newCheckerFromInlineDict(t *testing.T, affContent, dicContent string) *Checker {
	t.Helper()

	dir := t.TempDir()
	affPath := filepath.Join(dir, "custom.aff")
	dicPath := filepath.Join(dir, "custom.dic")

	if err := os.WriteFile(affPath, []byte(affContent), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dicPath, []byte(dicContent), 0600); err != nil {
		t.Fatal(err)
	}

	checker, err := NewChecker(UsingDictionaryByPath(dicPath, affPath))
	if err != nil {
		t.Fatalf("failed to create checker: %v", err)
	}

	return checker
}

func assertFormsEqual(t *testing.T, forms []string, expected map[string]struct{}) {
	t.Helper()

	if len(forms) != len(expected) {
		t.Fatalf("expected %d forms, got %d: %v", len(expected), len(forms), forms)
	}
	for _, form := range forms {
		if _, ok := expected[form]; !ok {
			t.Fatalf("unexpected form %q in %v", form, forms)
		}
	}
}
