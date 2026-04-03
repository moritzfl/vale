package check

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/nlp"
)

func makeSubstitution(def baseCheck) (*Substitution, error) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		return nil, err
	}

	rule, err := NewSubstitution(cfg, def, "")
	if err != nil {
		return nil, err
	}

	return rule, nil
}

func makeSubstitutionWithConfig(cfg *core.Config, def baseCheck) (*Substitution, error) {
	rule, err := NewSubstitution(cfg, def, "")
	if err != nil {
		return nil, err
	}

	return rule, nil
}

func writeMorphDict(t *testing.T, dir, name, aff, dic string) {
	t.Helper()

	affPath := filepath.Join(dir, name+".aff")
	dicPath := filepath.Join(dir, name+".dic")

	if err := os.WriteFile(affPath, []byte(aff), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dicPath, []byte(dic), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestConvertGroups(t *testing.T) {
	converted, err := convertCaptureGroups("change in(?: )?to the (.*) directory")
	if err != nil {
		t.Fatal(err)
	}

	expected := "change in(?: )?to the (?:.*) directory"
	if converted != expected {
		t.Fatalf("Expected '%s', got '%s'", expected, converted)
	}
}

func TestIsDeterministic(t *testing.T) {
	swap := map[string]interface{}{
		"extends":    "substitution",
		"name":       "Vale.Terms",
		"level":      "error",
		"message":    "Use '%s' instead of '%s'.",
		"scope":      "text",
		"ignorecase": true,
		"swap": map[string]string{
			"emnify iot supernetwork": "emnify IoT SuperNetwork",
			"emnify":                  "emnify",
		},
	}

	text := "EMnify IoT SuperNetwork"
	for i := 0; i < 100; i++ {
		rule, err := makeSubstitution(swap)
		if err != nil {
			t.Fatal(err)
		}

		actual, err := rule.Run(nlp.NewBlock(text, text, "text"), &core.File{}, &core.Config{})
		if err != nil {
			t.Fatal(err)
		}

		if len(actual) != 1 {
			t.Fatalf("expected 1 alert, found %d", len(actual))
		} else if actual[0].Match != "EMnify IoT SuperNetwork" {
			t.Fatalf("Loop %d: expected 'EMnify IoT SuperNetwork', found '%s'", i, actual[0].Match)
		}
	}
}

func TestRegex(t *testing.T) {
	swap := map[string]interface{}{
		"extends":    "substitution",
		"name":       "Vale.Terms",
		"level":      "error",
		"message":    "Use '%s' instead of '%s'.",
		"scope":      "text",
		"ignorecase": true,
		"swap": map[string]string{
			`(?:foo|bar)`: "sub",
		},
	}
	text := "foo"
	rule, err := makeSubstitution(swap)
	if err != nil {
		t.Fatal(err)
	}

	actual, err := rule.Run(nlp.NewBlock(text, text, "text"), &core.File{}, &core.Config{})
	if err != nil {
		t.Fatal(err)
	}

	expected := "Use 'sub' instead of 'foo'."
	message := actual[0].Message
	if message != expected {
		t.Fatalf("Expected message `%s`, got `%s`", expected, message)
	}
}

func TestRegexEscapedParens(t *testing.T) {
	swap := map[string]interface{}{
		"extends":    "substitution",
		"name":       "Vale.Terms",
		"level":      "error",
		"message":    "Use '%s' instead of '%s'.",
		"scope":      "text",
		"ignorecase": true,
		"swap": map[string]string{
			`(?!\()(?:foo|bar)(?!\))?`: "sub",
		},
	}
	text := "(foo)"
	rule, err := makeSubstitution(swap)
	if err != nil {
		t.Fatal(err)
	}

	actual, err := rule.Run(nlp.NewBlock(text, text, "text"), &core.File{}, &core.Config{})
	if err != nil {
		t.Fatal(err)
	}

	expected := "Use 'sub' instead of 'foo'."
	message := actual[0].Message
	if message != expected {
		t.Fatalf("Expected message `%s`, got `%s`", expected, message)
	}
}

func TestOptions(t *testing.T) {
	cases := map[string][]string{
		"foo|bar":     {"foo", "bar"},
		"foo|bar|baz": {"foo", "bar", "baz"},
		"|foo|":       {"foo"},
		`\|foo\|`:     {"|foo|"},
		`\|foo\||bar`: {"|foo|", "bar"},
		"foo|bar|":    {"foo", "bar"},
		"foo|":        {"foo"},
		"|":           {},
		`\|`:          {"|"},
	}

	for pattern, expected := range cases {
		actual := getOptions(pattern)
		if len(actual) != len(expected) {
			t.Fatalf("Expected %d options, got %v", len(expected), actual)
		}

		for i, opt := range expected {
			if actual[i] != opt {
				t.Fatalf("Expected '%s', got '%s'", opt, actual[i])
			}
		}
	}
}

func TestMorphologySubstitutionUsesDictionaries(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictPath := "../../testdata/styles/config/dictionaries"
	cfg.AddStylesPath("../../testdata/styles")

	swap := map[string]interface{}{
		"extends":      "substitution",
		"name":         "German.Gut",
		"level":        "warning",
		"message":      "Consider using '%s' instead of '%s'.",
		"scope":        "text",
		"ignorecase":   false,
		"morphology":   true,
		"dictionaries": []string{"de_DE"},
		"dicpath":      dictPath,
		"swap": map[string]string{
			"gut": "hervorragend",
		},
	}

	rule, err := makeSubstitutionWithConfig(cfg, swap)
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	if strings.Contains(rule.Pattern(), "guts") {
		t.Fatalf("expected German-only expansion, got %q", rule.Pattern())
	}

	tests := []struct {
		text        string
		expected    string
		shouldMatch bool
	}{
		{"Das ist gut.", "gut", true},
		{"Das ist eine gute Lösung.", "gute", true},
		{"Das ist gutes Wetter.", "gutes", true},
		{"Das ist guter Wein.", "guter", true},
		{"Das ist hervorragend.", "", false},
	}

	for _, test := range tests {
		actual, err := rule.Run(nlp.NewBlock(test.text, test.text, "text"), &core.File{}, cfg)
		if err != nil {
			t.Fatalf("Failed to run rule on '%s': %v", test.text, err)
		}

		if test.shouldMatch {
			if len(actual) != 1 {
				t.Errorf("Text '%s': expected 1 alert, got %d", test.text, len(actual))
			} else if actual[0].Match != test.expected {
				t.Errorf("Text '%s': expected match '%s', got '%s'", test.text, test.expected, actual[0].Match)
			}
		} else {
			if len(actual) != 0 {
				t.Errorf("Text '%s': expected no alerts, got %d", test.text, len(actual))
			}
		}
	}
}

func TestMorphologySubstitutionInflectsReplacement(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictDir := t.TempDir()
	writeMorphDict(
		t,
		dictDir,
		"de_DE",
		"SET ISO8859-1\nSFX A Y 1\nSFX A 0 e .\n",
		"2\ngut/A\nhervorragend/A\n",
	)

	rule, err := makeSubstitutionWithConfig(cfg, map[string]interface{}{
		"extends":      "substitution",
		"name":         "German.Gut",
		"level":        "warning",
		"message":      "Consider using '%s' instead of '%s'.",
		"scope":        "text",
		"ignorecase":   false,
		"morphology":   true,
		"dictionaries": []string{"de_DE"},
		"dicpath":      dictDir,
		"swap": map[string]string{
			"gut": "hervorragend",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	expected, err := subMsg(rule, 0, "gute")
	if err != nil {
		t.Fatal(err)
	}
	if expected != "hervorragende" {
		t.Fatalf("expected replacement 'hervorragende', got %q", expected)
	}
}

func TestMorphologySubstitutionUsesAffixLineageForReplacement(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictDir := t.TempDir()
	writeMorphDict(
		t,
		dictDir,
		"custom",
		"SET ISO8859-1\nSFX D Y 1\nSFX D e ed e\nSFX G Y 1\nSFX G e ing e\n",
		"2\noptimize/DG\nstreamline/GD\n",
	)

	rule, err := makeSubstitutionWithConfig(cfg, map[string]interface{}{
		"extends":      "substitution",
		"name":         "Custom.Optimize",
		"level":        "warning",
		"message":      "Consider using '%s' instead of '%s'.",
		"scope":        "text",
		"ignorecase":   false,
		"morphology":   true,
		"dictionaries": []string{"custom"},
		"dicpath":      dictDir,
		"swap": map[string]string{
			"optimize": "streamline",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	expected, err := subMsg(rule, 0, "optimized")
	if err != nil {
		t.Fatal(err)
	}
	if expected != "streamlined" {
		t.Fatalf("expected replacement 'streamlined', got %q", expected)
	}

	expected, err = subMsg(rule, 0, "optimizing")
	if err != nil {
		t.Fatal(err)
	}
	if expected != "streamlining" {
		t.Fatalf("expected replacement 'streamlining', got %q", expected)
	}
}

func TestMorphologySubstitutionDoesNotMatchReplacementInflections(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictDir := t.TempDir()
	writeMorphDict(
		t,
		dictDir,
		"de_DE",
		"SET UTF-8\nSFX A Y 1\nSFX A 0 e .\n",
		"2\ngut/A\nhervorragend/A\n",
	)

	rule, err := makeSubstitutionWithConfig(cfg, map[string]interface{}{
		"extends":      "substitution",
		"name":         "German.Gut",
		"level":        "warning",
		"message":      "Consider using '%s' instead of '%s'.",
		"scope":        "text",
		"ignorecase":   false,
		"morphology":   true,
		"dictionaries": []string{"de_DE"},
		"dicpath":      dictDir,
		"swap": map[string]string{
			"gut": "hervorragend",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	text := "Das ist eine hervorragende Lösung."
	alerts, err := rule.Run(nlp.NewBlock(text, text, "text"), &core.File{}, cfg)
	if err != nil {
		t.Fatalf("Failed to run rule: %v", err)
	}
	if len(alerts) != 0 {
		t.Fatalf("expected 0 alerts, got %d", len(alerts))
	}
}

func TestMorphologySubstitutionUsesRussianLineageIdentityForReplacement(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictDir := t.TempDir()
	writeMorphDict(
		t,
		dictDir,
		"ru_RU",
		strings.Join([]string{
			"SET UTF-8",
			"SFX A Y 6",
			"SFX A ый ое ый",
			"SFX A ый ая ый",
			"SFX A ий ее ий",
			"SFX A ий ая ий",
			"SFX A й е ый",
			"SFX A й е ий",
			"",
		}, "\n"),
		"2\nхороший/A\nотличный/A\n",
	)

	rule, err := makeSubstitutionWithConfig(cfg, map[string]interface{}{
		"extends":      "substitution",
		"name":         "Russian.Good",
		"level":        "warning",
		"message":      "Используйте '%s' вместо '%s'.",
		"scope":        "text",
		"ignorecase":   false,
		"morphology":   true,
		"dictionaries": []string{"ru_RU"},
		"dicpath":      dictDir,
		"swap": map[string]string{
			"хороший": "отличный",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	cases := map[string]string{
		"хорошая": "отличная",
		"хорошее": "отличное",
		"хорошие": "отличные",
	}

	for observed, expected := range cases {
		actual, msgErr := subMsg(rule, 0, observed)
		if msgErr != nil {
			t.Fatalf("Failed to build replacement for %q: %v", observed, msgErr)
		}
		if actual != expected {
			t.Fatalf("expected replacement %q for %q, got %q", expected, observed, actual)
		}
	}
}

func TestMorphologySubstitutionEscapesDictionaryRegexMetaCharacters(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictDir := t.TempDir()
	writeMorphDict(
		t,
		dictDir,
		"en_US",
		"SET ISO8859-1\nSFX A Y 1\nSFX A 0 s .\n",
		"1\nC++/A\n",
	)

	rule, err := makeSubstitutionWithConfig(cfg, map[string]interface{}{
		"extends":      "substitution",
		"name":         "Vale.Terms",
		"level":        "error",
		"message":      "Use '%s' instead of '%s'.",
		"scope":        "text",
		"ignorecase":   false,
		"nonword":      true,
		"morphology":   true,
		"dictionaries": []string{"en_US"},
		"dicpath":      dictDir,
		"swap": map[string]string{
			"C++": "Rust",
		},
	})
	if err != nil {
		t.Fatalf("failed to create rule: %v", err)
	}

	if !strings.Contains(rule.Pattern(), `C\+\+`) {
		t.Fatalf("expected escaped punctuation in compiled pattern, got %q", rule.Pattern())
	}

	// C++s might not be a real word, but its the best example I could think of :)
	if !strings.Contains(rule.Pattern(), `C\+\+s`) {
		t.Fatalf("expected escaped inflected form in compiled pattern, got %q", rule.Pattern())
	}

	alerts, err := rule.Run(nlp.NewBlock("We still ship C++s.", "We still ship C++s.", "text"), &core.File{}, cfg)
	if err != nil {
		t.Fatalf("failed to run rule: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if !strings.Contains(alerts[0].Message, "Rust") {
		t.Fatalf("unexpected message %q", alerts[0].Message)
	}
}

func TestSubstitutionWithoutMorphologyDoesNotMatchInflectedToken(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictDir := t.TempDir()
	writeMorphDict(
		t,
		dictDir,
		"de_DE",
		"SET ISO8859-1\nSFX A Y 1\nSFX A 0 e .\n",
		"2\ngut/A\nhervorragend/A\n",
	)

	rule, err := makeSubstitutionWithConfig(cfg, map[string]interface{}{
		"extends":      "substitution",
		"name":         "German.Gut",
		"level":        "warning",
		"message":      "Consider using '%s' instead of '%s'.",
		"scope":        "text",
		"ignorecase":   false,
		"morphology":   false,
		"dictionaries": []string{"de_DE"},
		"dicpath":      dictDir,
		"swap": map[string]string{
			"gut": "hervorragend",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	text := "Das ist eine gute Lösung."
	alerts, err := rule.Run(nlp.NewBlock(text, text, "text"), &core.File{}, cfg)
	if err != nil {
		t.Fatalf("Failed to run rule: %v", err)
	}
	if len(alerts) != 0 {
		t.Fatalf("expected 0 alerts, got %d", len(alerts))
	}
}

func TestMorphologyCheckerIsReusedAcrossRules(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictDir := t.TempDir()
	writeMorphDict(t, dictDir, "de_DE", "SET ISO8859-1\nSFX A Y 1\nSFX A 0 e .\n", "1\ngut/A\n")

	makeRule := func(path string) *Substitution {
		rule, ruleErr := NewSubstitution(cfg, map[string]interface{}{
			"extends":      "substitution",
			"name":         "German.Gut",
			"level":        "warning",
			"message":      "Consider using '%s' instead of '%s'.",
			"scope":        "text",
			"ignorecase":   false,
			"morphology":   true,
			"dictionaries": []string{"de_DE"},
			"dicpath":      dictDir,
			"swap": map[string]string{
				"gut": "hervorragend",
			},
		}, path)
		if ruleErr != nil {
			t.Fatalf("Failed to create rule: %v", ruleErr)
		}
		return rule
	}

	ruleA := makeRule("styles/German/GutA.yml")
	ruleB := makeRule("styles/German/GutB.yml")

	checkerA, err := ruleA.makeMorphologyChecker(cfg)
	if err != nil {
		t.Fatalf("Failed to create first checker: %v", err)
	}
	checkerB, err := ruleB.makeMorphologyChecker(cfg)
	if err != nil {
		t.Fatalf("Failed to create second checker: %v", err)
	}

	if checkerA == nil || checkerB == nil {
		t.Fatalf("expected non-nil checkers, got %v and %v", checkerA, checkerB)
	}
	if checkerA != checkerB {
		t.Fatal("expected morphology checker to be reused across rules")
	}
}

func TestMorphologyCheckerKeepsInternalDefaultsDistinct(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	stylesDir := t.TempDir()
	if err = os.MkdirAll(filepath.Join(stylesDir, core.DictDir), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMorphDict(
		t,
		filepath.Join(stylesDir, core.DictDir),
		"custom",
		"SET ISO8859-1\nSFX A Y 1\nSFX A 0 e .\n",
		"1\ngut/A\n",
	)
	cfg.AddStylesPath(stylesDir)

	makeRule := func(path string) *Substitution {
		rule, ruleErr := NewSubstitution(cfg, map[string]interface{}{
			"extends":    "substitution",
			"name":       "German.Gut",
			"level":      "warning",
			"message":    "Consider using '%s' instead of '%s'.",
			"scope":      "text",
			"ignorecase": false,
			"morphology": true,
			"swap": map[string]string{
				"gut": "hervorragend",
			},
		}, path)
		if ruleErr != nil {
			t.Fatalf("Failed to create rule: %v", ruleErr)
		}
		return rule
	}

	externalRule := makeRule("styles/German/Gut.yml")
	internalRule := makeRule("internal")

	externalChecker, err := externalRule.makeMorphologyChecker(cfg)
	if err != nil {
		t.Fatalf("Failed to create external checker: %v", err)
	}
	internalChecker, err := internalRule.makeMorphologyChecker(cfg)
	if err != nil {
		t.Fatalf("Failed to create internal checker: %v", err)
	}

	if externalChecker == internalChecker {
		t.Fatal("expected internal default-path checker to remain distinct")
	}
}

func TestMorphologyCheckerSeparatesRelativeDicpathsByWorkingDirectory(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	rootA := t.TempDir()
	rootB := t.TempDir()

	dictA := filepath.Join(rootA, "dict")
	dictB := filepath.Join(rootB, "dict")
	if err = os.MkdirAll(dictA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(dictB, 0o755); err != nil {
		t.Fatal(err)
	}

	writeMorphDict(t, dictA, "custom", "SET ISO8859-1\nSFX A Y 1\nSFX A 0 e .\n", "1\ngut/A\n")
	writeMorphDict(t, dictB, "custom", "SET ISO8859-1\nSFX A Y 1\nSFX A 0 er .\n", "1\ngut/A\n")

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	makeRule := func() *Substitution {
		rule, ruleErr := makeSubstitutionWithConfig(cfg, map[string]interface{}{
			"extends":      "substitution",
			"name":         "German.Gut",
			"level":        "warning",
			"message":      "Consider using '%s' instead of '%s'.",
			"scope":        "text",
			"ignorecase":   false,
			"morphology":   true,
			"dictionaries": []string{"custom"},
			"dicpath":      "dict",
			"swap": map[string]string{
				"gut": "hervorragend",
			},
		})
		if ruleErr != nil {
			t.Fatalf("Failed to create rule: %v", ruleErr)
		}
		return rule
	}

	if err = os.Chdir(rootA); err != nil {
		t.Fatal(err)
	}
	ruleA := makeRule()
	checkerA, err := ruleA.makeMorphologyChecker(cfg)
	if err != nil {
		t.Fatalf("Failed to create first checker: %v", err)
	}

	if err = os.Chdir(rootB); err != nil {
		t.Fatal(err)
	}
	ruleB := makeRule()
	checkerB, err := ruleB.makeMorphologyChecker(cfg)
	if err != nil {
		t.Fatalf("Failed to create second checker: %v", err)
	}

	if checkerA == checkerB {
		t.Fatal("expected distinct morphology checkers for different resolved dicpaths")
	}
}

func TestMorphologyCheckerConcurrentCreation(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictDir := t.TempDir()
	writeMorphDict(t, dictDir, "de_DE", "SET ISO8859-1\nSFX A Y 1\nSFX A 0 e .\n", "1\ngut/A\n")

	rule, err := makeSubstitutionWithConfig(cfg, map[string]interface{}{
		"extends":      "substitution",
		"name":         "German.Gut",
		"level":        "warning",
		"message":      "Consider using '%s' instead of '%s'.",
		"scope":        "text",
		"ignorecase":   false,
		"morphology":   true,
		"dictionaries": []string{"de_DE"},
		"dicpath":      dictDir,
		"swap": map[string]string{
			"gut": "hervorragend",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	const workers = 16
	results := make([]any, workers)
	start := make(chan struct{})
	errs := make(chan error, workers)

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		idx := i
		go func() {
			defer wg.Done()
			<-start

			checker, checkerErr := rule.makeMorphologyChecker(cfg)
			if checkerErr != nil {
				errs <- checkerErr
				return
			}
			results[idx] = checker
		}()
	}

	close(start)
	wg.Wait()
	close(errs)

	for checkerErr := range errs {
		if checkerErr != nil {
			t.Fatalf("unexpected checker creation error: %v", checkerErr)
		}
	}

	first := results[0]
	if first == nil {
		t.Fatal("expected non-nil checker")
	}
	for i := 1; i < workers; i++ {
		if results[i] != first {
			t.Fatalf("expected checker %d to match first cached checker", i)
		}
	}
}

func TestMorphologyCheckerUsesLazyDictionaryLoading(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictDir := t.TempDir()
	writeMorphDict(
		t,
		dictDir,
		"en_US",
		"SET ISO8859-1\nSFX A Y 2\nSFX A 0 d e\nSFX A e ing e\n",
		"1\nutilize/A\n",
	)

	rule, err := makeSubstitutionWithConfig(cfg, map[string]interface{}{
		"extends":      "substitution",
		"name":         "English.Utilize",
		"level":        "warning",
		"message":      "Consider using '%s' instead of '%s'.",
		"scope":        "text",
		"ignorecase":   false,
		"morphology":   true,
		"dictionaries": []string{"en_US"},
		"dicpath":      dictDir,
		"swap": map[string]string{
			"utilize": "use",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	checker, err := rule.makeMorphologyChecker(cfg)
	if err != nil {
		t.Fatalf("failed to get morphology checker: %v", err)
	}

	// Terminology morphology should avoid eager full-dictionary expansion.
	if got := len(checker.Dict(0)); got != 1 {
		t.Fatalf("expected 1 headword in lazy morphology mode, got %d", got)
	}

	alerts, err := rule.Run(nlp.NewBlock("We utilized this method.", "We utilized this method.", "text"), &core.File{}, cfg)
	if err != nil {
		t.Fatalf("failed to run rule: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
}

func TestMorphologySubstitutionRequiresExplicitDictionariesLikeSpelling(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictDir := t.TempDir()
	writeMorphDict(t, dictDir, "custom", "SET ISO8859-1\nSFX A Y 1\nSFX A 0 e .\n", "1\nfoobase/A\n")

	withoutDictionaries, err := makeSubstitutionWithConfig(cfg, map[string]interface{}{
		"extends":    "substitution",
		"name":       "Custom.Foo",
		"level":      "warning",
		"message":    "Consider using '%s' instead of '%s'.",
		"scope":      "text",
		"ignorecase": false,
		"morphology": true,
		"dicpath":    dictDir,
		"swap": map[string]string{
			"foobase": "replacement",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create rule without dictionaries: %v", err)
	}

	text := "Das ist foobasee."
	alerts, err := withoutDictionaries.Run(nlp.NewBlock(text, text, "text"), &core.File{}, cfg)
	if err != nil {
		t.Fatalf("Failed to run rule without dictionaries: %v", err)
	}
	if len(alerts) != 0 {
		t.Fatalf("expected no alerts without explicit dictionaries, got %d", len(alerts))
	}

	withDictionary, err := makeSubstitutionWithConfig(cfg, map[string]interface{}{
		"extends":      "substitution",
		"name":         "Custom.Foo",
		"level":        "warning",
		"message":      "Consider using '%s' instead of '%s'.",
		"scope":        "text",
		"ignorecase":   false,
		"morphology":   true,
		"dicpath":      dictDir,
		"dictionaries": []string{"custom"},
		"swap": map[string]string{
			"foobase": "replacement",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create rule with dictionaries: %v", err)
	}

	alerts, err = withDictionary.Run(nlp.NewBlock(text, text, "text"), &core.File{}, cfg)
	if err != nil {
		t.Fatalf("Failed to run rule with dictionaries: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert with explicit dictionary, got %d", len(alerts))
	}
}

func TestMorphologySubstitutionCombinesExplicitDictionaries(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictDir := t.TempDir()
	writeMorphDict(t, dictDir, "de_DE", "SET ISO8859-1\nSFX A Y 1\nSFX A 0 e .\n", "1\ngut/A\n")
	writeMorphDict(t, dictDir, "de_AT", "SET ISO8859-1\nSFX B Y 1\nSFX B 0 er .\n", "1\ngut/B\n")

	rule, err := makeSubstitutionWithConfig(cfg, map[string]interface{}{
		"extends":      "substitution",
		"name":         "German.Gut",
		"level":        "warning",
		"message":      "Consider using '%s' instead of '%s'.",
		"scope":        "text",
		"ignorecase":   false,
		"morphology":   true,
		"dictionaries": []string{"de_DE", "de_AT"},
		"dicpath":      dictDir,
		"swap": map[string]string{
			"gut": "hervorragend",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	for _, text := range []string{"Das ist eine gute Idee.", "Das ist guter Stil."} {
		alerts, err := rule.Run(nlp.NewBlock(text, text, "text"), &core.File{}, cfg)
		if err != nil {
			t.Fatalf("Failed to run rule on %q: %v", text, err)
		}
		if len(alerts) != 1 {
			t.Fatalf("expected 1 alert for %q, got %d", text, len(alerts))
		}
	}
}

func TestMorphologySubstitutionPrefersSameDictionaryReplacement(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictDir := t.TempDir()
	writeMorphDict(
		t,
		dictDir,
		"dictA",
		"SET UTF-8\nSFX D Y 1\nSFX D e ed e\n",
		"1\nstreamline/D\n",
	)
	writeMorphDict(
		t,
		dictDir,
		"dictB",
		"SET UTF-8\nSFX D Y 1\nSFX D e en e\n",
		"2\noptimize/D\nstreamline/D\n",
	)

	rule, err := makeSubstitutionWithConfig(cfg, map[string]interface{}{
		"extends":      "substitution",
		"name":         "Custom.Optimize",
		"level":        "warning",
		"message":      "Consider using '%s' instead of '%s'.",
		"scope":        "text",
		"ignorecase":   false,
		"morphology":   true,
		"dictionaries": []string{"dictA", "dictB"},
		"dicpath":      dictDir,
		"swap": map[string]string{
			"optimize": "streamline",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	expected, err := subMsg(rule, 0, "optimizen")
	if err != nil {
		t.Fatal(err)
	}
	if expected != "streamlinen" {
		t.Fatalf("expected same-dictionary replacement 'streamlinen', got %q", expected)
	}

	text := "We optimizen the workflow."
	alerts, err := rule.Run(nlp.NewBlock(text, text, "text"), &core.File{}, cfg)
	if err != nil {
		t.Fatalf("Failed to run rule: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if !strings.Contains(alerts[0].Message, "streamlinen") {
		t.Fatalf("expected message to prefer same-dictionary replacement, got %q", alerts[0].Message)
	}
}

func TestMorphologySubstitutionFallsBackToGlobalReplacementWhenSameDictionaryMisses(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictDir := t.TempDir()
	writeMorphDict(
		t,
		dictDir,
		"dictA",
		"SET UTF-8\nSFX G Y 1\nSFX G e ing e\n",
		"1\nstreamline/G\n",
	)
	writeMorphDict(
		t,
		dictDir,
		"dictB",
		"SET UTF-8\nSFX G Y 1\nSFX G e ing e\nSFX D Y 1\nSFX D e ed e\n",
		"2\noptimize/G\nstreamline/D\n",
	)

	rule, err := makeSubstitutionWithConfig(cfg, map[string]interface{}{
		"extends":      "substitution",
		"name":         "Custom.OptimizeFallback",
		"level":        "warning",
		"message":      "Consider using '%s' instead of '%s'.",
		"scope":        "text",
		"ignorecase":   false,
		"morphology":   true,
		"dictionaries": []string{"dictA", "dictB"},
		"dicpath":      dictDir,
		"swap": map[string]string{
			"optimize": "streamline",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	expected, err := subMsg(rule, 0, "optimizing")
	if err != nil {
		t.Fatal(err)
	}
	if expected != "streamlining" {
		t.Fatalf("expected global fallback replacement 'streamlining', got %q", expected)
	}

	text := "We are optimizing the workflow."
	alerts, err := rule.Run(nlp.NewBlock(text, text, "text"), &core.File{}, cfg)
	if err != nil {
		t.Fatalf("Failed to run rule: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if !strings.Contains(alerts[0].Message, "streamlining") {
		t.Fatalf("expected message to use global fallback replacement, got %q", alerts[0].Message)
	}
}

func TestMorphologySubstitutionPhraseExpansion(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictPath := "../../testdata/styles/config/dictionaries"
	cfg.AddStylesPath("../../testdata/styles")

	rule, err := makeSubstitutionWithConfig(cfg, map[string]interface{}{
		"extends":      "substitution",
		"name":         "German.Phrase",
		"level":        "warning",
		"message":      "Consider using '%s' instead of '%s'.",
		"scope":        "text",
		"ignorecase":   false,
		"morphology":   true,
		"dictionaries": []string{"de_DE"},
		"dicpath":      dictPath,
		"swap": map[string]string{
			"gut Lösung": "hervorragende Lösung",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	alerts, err := rule.Run(
		nlp.NewBlock("Das ist eine gute Lösung.", "Das ist eine gute Lösung.", "text"),
		&core.File{},
		cfg,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].Match != "gute Lösung" {
		t.Fatalf("expected match 'gute Lösung', got %q", alerts[0].Match)
	}
}

func TestMorphologySubstitutionLiteralAlternationExpansion(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictDir := t.TempDir()
	writeMorphDict(
		t,
		dictDir,
		"en_US",
		"SET ISO8859-1\nSFX A Y 1\nSFX A 0 s .\n",
		"3\nsetting/A\noption/A\npreference/A\n",
	)

	rule, err := makeSubstitutionWithConfig(cfg, map[string]interface{}{
		"extends":      "substitution",
		"name":         "English.Preference",
		"level":        "warning",
		"message":      "Consider using '%s' instead of '%s'.",
		"scope":        "text",
		"ignorecase":   false,
		"morphology":   true,
		"dictionaries": []string{"en_US"},
		"dicpath":      dictDir,
		"swap": map[string]string{
			"setting|option": "preference",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	cases := []struct {
		text    string
		match   string
		message string
	}{
		{
			text:    "These settings are configurable.",
			match:   "settings",
			message: "Consider using 'preferences' instead of 'settings'.",
		},
		{
			text:    "These options are configurable.",
			match:   "options",
			message: "Consider using 'preferences' instead of 'options'.",
		},
	}

	for _, tc := range cases {
		alerts, runErr := rule.Run(
			nlp.NewBlock(tc.text, tc.text, "text"),
			&core.File{},
			cfg,
		)
		if runErr != nil {
			t.Fatal(runErr)
		}
		if len(alerts) != 1 {
			t.Fatalf("expected 1 alert for %q, got %d", tc.text, len(alerts))
		}
		if alerts[0].Match != tc.match {
			t.Fatalf("expected match %q, got %q", tc.match, alerts[0].Match)
		}
		if alerts[0].Message != tc.message {
			t.Fatalf("unexpected message for %q: %q", tc.text, alerts[0].Message)
		}
	}
}

func TestMorphologySubstitutionMarkedRegexGroupExpansion(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictDir := t.TempDir()
	writeMorphDict(
		t,
		dictDir,
		"en_US",
		"SET ISO8859-1\nSFX A Y 1\nSFX A 0 s .\n",
		"3\nsetting/A\noption/A\npreference/A\n",
	)

	rule, err := makeSubstitutionWithConfig(cfg, map[string]interface{}{
		"extends":      "substitution",
		"name":         "English.PreferenceMarked",
		"level":        "warning",
		"message":      "Consider using '%s' instead of '%s'.",
		"scope":        "text",
		"ignorecase":   false,
		"morphology":   true,
		"dictionaries": []string{"en_US"},
		"dicpath":      dictDir,
		"swap": map[string]string{
			"(?<morph_term>setting|option)": "preference",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	cases := []struct {
		text    string
		match   string
		message string
	}{
		{
			text:    "These settings are configurable.",
			match:   "settings",
			message: "Consider using 'preferences' instead of 'settings'.",
		},
		{
			text:    "These options are configurable.",
			match:   "options",
			message: "Consider using 'preferences' instead of 'options'.",
		},
	}

	for _, tc := range cases {
		alerts, runErr := rule.Run(
			nlp.NewBlock(tc.text, tc.text, "text"),
			&core.File{},
			cfg,
		)
		if runErr != nil {
			t.Fatal(runErr)
		}
		if len(alerts) != 1 {
			t.Fatalf("expected 1 alert for %q, got %d", tc.text, len(alerts))
		}
		if alerts[0].Match != tc.match {
			t.Fatalf("expected match %q, got %q", tc.match, alerts[0].Match)
		}
		if alerts[0].Message != tc.message {
			t.Fatalf("unexpected message for %q: %q", tc.text, alerts[0].Message)
		}
	}
}

func TestMorphologySubstitutionPhraseWithPunctuation(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictDir := t.TempDir()
	writeMorphDict(
		t,
		dictDir,
		"en_US",
		"SET ISO8859-1\nSFX A Y 1\nSFX A 0 s .\n",
		"3\nsetting/A\noption/A\npreference/A\n",
	)

	rule, err := makeSubstitutionWithConfig(cfg, map[string]interface{}{
		"extends":      "substitution",
		"name":         "English.PreferencePhrase",
		"level":        "warning",
		"message":      "Consider using '%s' instead of '%s'.",
		"scope":        "text",
		"ignorecase":   false,
		"morphology":   true,
		"dictionaries": []string{"en_US"},
		"dicpath":      dictDir,
		"swap": map[string]string{
			"setting, option": "preference, preference",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}

	alerts, runErr := rule.Run(
		nlp.NewBlock(
			"These settings, options are configurable.",
			"These settings, options are configurable.",
			"text",
		),
		&core.File{},
		cfg,
	)
	if runErr != nil {
		t.Fatal(runErr)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].Match != "settings, options" {
		t.Fatalf("expected match %q, got %q", "settings, options", alerts[0].Match)
	}
	if alerts[0].Message != "Consider using 'preferences, preferences' instead of 'settings, options'." {
		t.Fatalf("unexpected message: %q", alerts[0].Message)
	}
}

func TestMorphologySubstitutionRegexPatternSkipsWordTemplateSplit(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictDir := t.TempDir()
	writeMorphDict(
		t,
		dictDir,
		"en_US",
		"SET ISO8859-1\nSFX A Y 1\nSFX A 0 s .\n",
		"2\nsetting/A\noption/A\n",
	)

	rule, err := makeSubstitutionWithConfig(cfg, map[string]interface{}{
		"extends":      "substitution",
		"name":         "English.RegexSkip",
		"level":        "warning",
		"message":      "Use '%s' instead of '%s'.",
		"scope":        "text",
		"ignorecase":   false,
		"morphology":   true,
		"dictionaries": []string{"en_US"},
		"dicpath":      dictDir,
		"swap": map[string]string{
			`(?<!MyProduct\s)setting option`: "MyProduct preference",
		},
	})
	if err != nil {
		t.Fatalf("failed to create rule: %v", err)
	}

	alerts, runErr := rule.Run(
		nlp.NewBlock("setting options are available.", "setting options are available.", "text"),
		&core.File{},
		cfg,
	)
	if runErr != nil {
		t.Fatal(runErr)
	}
	if len(alerts) != 0 {
		t.Fatalf("expected no alerts, got %d", len(alerts))
	}
}

func TestMorphologyInvalidDicpath(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	cfg.AddStylesPath("../../testdata/styles")

	_, err = makeSubstitutionWithConfig(cfg, map[string]interface{}{
		"extends":    "substitution",
		"name":       "German.Gut",
		"level":      "warning",
		"message":    "Consider using '%s' instead of '%s'.",
		"scope":      "text",
		"ignorecase": false,
		"morphology": true,
		"dicpath":    "../../testdata/styles/config/missing",
		"swap": map[string]string{
			"gut": "hervorragend",
		},
	})
	if err == nil {
		t.Fatal("expected invalid dicpath to return an error")
	}
}

func TestExpandForMorphology(t *testing.T) {
	tests := []struct {
		word     string
		expected string
	}{
		{"gut", "gut"},
		{"unknown", "unknown"},
	}

	for _, test := range tests {
		actual := expandForMorphology(test.word, nil, "")
		if actual != test.expected {
			t.Errorf("expandForMorphology(%q) = %q, expected %q", test.word, actual, test.expected)
		}
	}
}

func TestExpandForMorphologyEscapesDictionaryRegexMetaCharacters(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictDir := t.TempDir()
	writeMorphDict(
		t,
		dictDir,
		"en_US",
		"SET ISO8859-1\nSFX A Y 1\nSFX A 0 s .\n",
		"1\nC++/A\n",
	)

	checker, err := makeSpeller(&Spelling{
		Dictionaries: []string{"en_US"},
		Dicpath:      dictDir,
	}, cfg, "")
	if err != nil {
		t.Fatalf("failed to create checker: %v", err)
	}
	// C++s might not be a real word, but its the best example I could think of :)
	actual := expandForMorphology("C++", checker, cfg.WordTemplate)
	if actual != `(?:C\+\+|C\+\+s)` {
		t.Fatalf("expandForMorphology returned %q", actual)
	}
}
