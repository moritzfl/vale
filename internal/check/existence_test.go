package check

import (
	"testing"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/nlp"
)

func makeExistence(tokens []string) (*Existence, error) {
	def := baseCheck{"tokens": tokens}

	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		return nil, err
	}

	rule, err := NewExistence(cfg, def, "")
	if err != nil {
		return nil, err
	}

	return &rule, nil
}

func makeExistenceWithConfig(cfg *core.Config, def baseCheck) (*Existence, error) {
	rule, err := NewExistence(cfg, def, "")
	if err != nil {
		return nil, err
	}

	return &rule, nil
}

func TestExistence(t *testing.T) {
	rule, err := makeExistence([]string{"test"})
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	file, err := core.NewFile("", cfg)
	if err != nil {
		t.Fatal(err)
	}

	alerts, _ := rule.Run(nlp.NewBlock("", "This is a test.", ""), file, cfg)
	if len(alerts) != 1 {
		t.Errorf("expected one alert, not %v", alerts)
	}
}

func TestExistenceMorphologyMatchesInflectedToken(t *testing.T) {
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

	rule, err := makeExistenceWithConfig(cfg, baseCheck{
		"tokens":       []string{"utilize"},
		"morphology":   true,
		"dictionaries": []string{"en_US"},
		"dicpath":      dictDir,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, text := range []string{"We utilized this approach.", "We are utilizing this approach."} {
		alerts, runErr := rule.Run(nlp.NewBlock("", text, ""), &core.File{}, cfg)
		if runErr != nil {
			t.Fatalf("run failed for %q: %v", text, runErr)
		}
		if len(alerts) != 1 {
			t.Fatalf("expected 1 alert for %q, got %d", text, len(alerts))
		}
	}
}

func TestExistenceWithoutMorphologyDoesNotMatchInflectedToken(t *testing.T) {
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

	rule, err := makeExistenceWithConfig(cfg, baseCheck{
		"tokens":       []string{"utilize"},
		"morphology":   false,
		"dictionaries": []string{"en_US"},
		"dicpath":      dictDir,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, text := range []string{"We utilized this approach.", "We are utilizing this approach."} {
		alerts, runErr := rule.Run(nlp.NewBlock("", text, ""), &core.File{}, cfg)
		if runErr != nil {
			t.Fatalf("run failed for %q: %v", text, runErr)
		}
		if len(alerts) != 0 {
			t.Fatalf("expected 0 alerts for %q, got %d", text, len(alerts))
		}
	}
}

func TestExistenceMorphologyExpandsLiteralAlternation(t *testing.T) {
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

	rule, err := makeExistenceWithConfig(cfg, baseCheck{
		"tokens":       []string{"setting|option"},
		"morphology":   true,
		"dictionaries": []string{"en_US"},
		"dicpath":      dictDir,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, text := range []string{
		"These settings are configurable.",
		"These options are configurable.",
	} {
		alerts, runErr := rule.Run(nlp.NewBlock("", text, ""), &core.File{}, cfg)
		if runErr != nil {
			t.Fatalf("run failed for %q: %v", text, runErr)
		}
		if len(alerts) != 1 {
			t.Fatalf("expected 1 alert for %q, got %d", text, len(alerts))
		}
	}
}

func TestExistenceMorphologyExpandsLiteralPhraseAlternation(t *testing.T) {
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
		"3\nsetting/A\nwindow/A\noption/A\n",
	)

	rule, err := makeExistenceWithConfig(cfg, baseCheck{
		"tokens":       []string{"setting option|window option"},
		"morphology":   true,
		"dictionaries": []string{"en_US"},
		"dicpath":      dictDir,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, text := range []string{
		"These settings options are configurable.",
		"These windows options are configurable.",
	} {
		alerts, runErr := rule.Run(nlp.NewBlock("", text, ""), &core.File{}, cfg)
		if runErr != nil {
			t.Fatalf("run failed for %q: %v", text, runErr)
		}
		if len(alerts) != 1 {
			t.Fatalf("expected 1 alert for %q, got %d", text, len(alerts))
		}
	}
}

func TestExistenceMorphologyExpandsMarkedRegexGroup(t *testing.T) {
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

	rule, err := makeExistenceWithConfig(cfg, baseCheck{
		"tokens":       []string{"(?<morph_term>setting|option)"},
		"morphology":   true,
		"dictionaries": []string{"en_US"},
		"dicpath":      dictDir,
	})
	if err != nil {
		t.Fatal(err)
	}

	alerts, runErr := rule.Run(
		nlp.NewBlock("", "These settings are configurable.", ""),
		&core.File{},
		cfg,
	)
	if runErr != nil {
		t.Fatal(runErr)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
}

func FuzzExistenceInit(f *testing.F) {
	f.Add("hello")
	f.Fuzz(func(_ *testing.T, s string) {
		_, _ = makeExistence([]string{s})
	})
}

func FuzzExistence(f *testing.F) {
	rule, err := makeExistence([]string{"test"})
	if err != nil {
		f.Fatal(err)
	}

	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		f.Fatal(err)
	}

	file, err := core.NewFile("", cfg)
	if err != nil {
		f.Fatal(err)
	}

	f.Add("hello")
	f.Fuzz(func(_ *testing.T, s string) {
		_, _ = rule.Run(nlp.NewBlock("", s, ""), file, cfg)
	})
}
