package spell

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/errata-ai/vale/v3/internal/system"
)

//go:embed data/en_US-web.aff
var defaultAff []byte

//go:embed data/en_US-web.dic
var defaultDic []byte

var defaultOpts = Options{
	path: os.Getenv("DICPATH"),
	load: false,

	system: os.Getenv("DICPATH"),
}

// Options controls the checker-creation process:
type Options struct {
	path        string
	defaultPath string
	system      string
	names       []string
	dics        []dictionary
	load        bool
	lazyMorph   bool
}

// A CheckerOption is a setting that changes the checker-creation process.
type CheckerOption func(opts *Options)

// WithPath specifies the location of Hunspell-compatible dictionaries.
func WithPath(path string) CheckerOption {
	return func(opts *Options) {
		opts.path = path
	}
}

// WithDefault specifies if Vale's default dictionary should be loaded.
func WithDefault(load bool) CheckerOption {
	return func(opts *Options) {
		opts.load = load
	}
}

// WithDefaultPath specifies a path from which all dictionaries should be
// loaded.
func WithDefaultPath(path string) CheckerOption {
	return func(opts *Options) {
		opts.defaultPath = path
	}
}

// UsingDictionary loads the given Hunspell-compatible dictionary.
func UsingDictionary(name string) CheckerOption {
	return func(opts *Options) {
		opts.names = append(opts.names, name)
	}
}

// UsingDictionaryByPath loads the given Hunspell-compatible dictionary using
// the given local paths.
func UsingDictionaryByPath(dic, aff string) CheckerOption {
	return func(opts *Options) {
		opts.dics = append(opts.dics, dictionary{dic, aff})
	}
}

// WithLazyMorphology enables lazy dictionary expansion where inflections are
// derived on demand and cached per queried lemma.
func WithLazyMorphology() CheckerOption {
	return func(opts *Options) {
		opts.lazyMorph = true
	}
}

// Checker is a spell-checker based on multiple dictionaries.
type Checker struct {
	options  Options
	checkers []*goSpell
}

// Inflection represents one expanded word form and the affix lineage that
// produced it.
type Inflection struct {
	Form       string
	Lineage    string
	LineageKey string
}

// NewChecker creates a spell checker from multiple
// Hunspell-compatible dictionaries.
func NewChecker(options ...CheckerOption) (*Checker, error) {
	base := defaultOpts
	for _, applyOpt := range options {
		applyOpt(&base)
	}

	checker := Checker{options: base}
	for _, name := range base.names {
		if err := checker.loadDic(name); err != nil {
			return &checker, err
		}
	}

	for _, entry := range base.dics {
		c, err := newGoSpellWithOptions(entry.aff, entry.dic, goSpellLoadOptions{
			lazyMorphology: base.lazyMorph,
		})
		if err != nil {
			return &checker, err
		}
		checker.checkers = append(checker.checkers, c)
	}

	if len(checker.checkers) == 0 || base.load {
		// use default dictionary ...
		aff := bytes.NewReader(defaultAff)
		dic := bytes.NewReader(defaultDic)

		c, err := newGoSpellReaderWithOptions(aff, dic, goSpellLoadOptions{
			lazyMorphology: base.lazyMorph,
		})
		if err != nil {
			return &checker, err
		}

		checker.checkers = append(checker.checkers, c)
	}

	if base.defaultPath != "" {
		// load all dictionaries from the given path ...
		files, err := filepath.Glob(base.defaultPath + "/*.dic")
		if err != nil {
			return &checker, err
		}

		for _, f := range files {
			name := filepath.Base(f)
			name = name[:len(name)-4]
			if loadErr := checker.loadDic(name); loadErr != nil {
				return &checker, loadErr
			}
		}
	}

	return &checker, nil
}

// Spell checks to see if a given word is in the internal dictionaries.
func (m *Checker) Spell(word string) bool {
	for _, checker := range m.checkers {
		if checker.spell(word) {
			return true
		}
	}
	return false
}

// Expand returns all inflected forms of a given word based on the
// dictionary's affix rules. It merges forms from all loaded dictionaries
// that recognize the provided word.
func (m *Checker) Expand(word string) []string {
	inflections := m.ExpandWithLineage(word)
	seen := make(map[string]struct{}, len(inflections))
	forms := make([]string, 0, len(inflections))
	for _, inflection := range inflections {
		if _, ok := seen[inflection.Form]; ok {
			continue
		}
		seen[inflection.Form] = struct{}{}
		forms = append(forms, inflection.Form)
	}

	return forms
}

// ExpandWithLineage returns all inflected forms of a given word and tracks the
// affix lineage used to derive each form.
func (m *Checker) ExpandWithLineage(word string) []Inflection {
	seen := make(map[string]struct{})
	inflections := []Inflection{}

	for _, checker := range m.checkers {
		expanded := checker.ExpandWithLineage(word)
		if len(expanded) == 1 && expanded[0].Form == word && expanded[0].Lineage == "" {
			continue
		}

		for _, inflection := range expanded {
			key := inflection.Form + "\x00" + inflection.Lineage
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			inflections = append(inflections, inflection)
		}
	}

	if len(inflections) == 0 {
		return []Inflection{{Form: word}}
	}

	return inflections
}

// Suggest returns a list of suggestions for a given word.
func (m *Checker) Suggest(word string) []string {
	ranks := []wordMatch{}
	for _, checker := range m.checkers {
		ranks = append(ranks, checker.suggest(word)...)
	}

	sort.Slice(ranks, func(i, j int) bool {
		return ranks[i].score > ranks[j].score
	})

	suggestions := []string{}
	for i, r := range ranks {
		if i > 5 {
			break
		}
		suggestions = append(suggestions, r.word)
	}

	return suggestions
}

// Dict returns the underlying dictionary for the provided index.
func (m *Checker) Dict(i int) map[string]struct{} {
	return m.checkers[i].dict
}

// Convert performs character substitutions (ICONV).
func (m *Checker) Convert(s string) string {
	for _, checker := range m.checkers {
		s = checker.inputConversion([]byte(s))
	}
	return s
}

// AddWordListFile reads in a word list file
func (m *Checker) AddWordListFile(name string) error {
	for _, checker := range m.checkers {
		_, err := checker.addWordListFile(name)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *Checker) readAsset(name string) (string, error) {
	roots := []string{
		m.options.defaultPath,
		m.options.path,
		m.options.system,
	}

	for _, p := range roots {
		if p == "" {
			continue
		}

		option := filepath.Join(p, name)
		if system.FileExists(option) {
			return option, nil
		}

		ln, err := os.Readlink(option)
		if err != nil {
			return "", err
		} else if system.FileExists(ln) {
			return ln, nil
		}
	}

	return "", fmt.Errorf("'%s' not found in %v", name, roots)
}

func (m *Checker) loadDic(name string) error {
	dicPath, err := m.readAsset(name + ".dic")
	if err != nil {
		return err
	}

	affPath, err := m.readAsset(name + ".aff")
	if err != nil {
		return err
	}

	s, err := newGoSpellWithOptions(affPath, dicPath, goSpellLoadOptions{
		lazyMorphology: m.options.lazyMorph,
	})
	if err != nil {
		return err
	}
	m.checkers = append(m.checkers, s)

	return nil
}
