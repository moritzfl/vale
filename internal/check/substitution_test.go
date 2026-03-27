package check

import (
	"os"
	"path/filepath"
	"strings"
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

func TestMorphologyCheckerIsReusedAcrossRules(t *testing.T) {
	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	dictDir := t.TempDir()
	writeMorphDict(t, dictDir, "de_DE", "SET ISO8859-1\nSFX A Y 1\nSFX A 0 e .\n", "1\ngut/A\n")

	makeRule := func() *Substitution {
		rule, ruleErr := makeSubstitutionWithConfig(cfg, map[string]interface{}{
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
		if ruleErr != nil {
			t.Fatalf("Failed to create rule: %v", ruleErr)
		}
		return rule
	}

	ruleA := makeRule()
	ruleB := makeRule()

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
		actual := expandForMorphology(test.word, nil)
		if actual != test.expected {
			t.Errorf("expandForMorphology(%q) = %q, expected %q", test.word, actual, test.expected)
		}
	}
}
