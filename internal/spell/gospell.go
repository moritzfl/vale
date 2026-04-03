package spell

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/adrg/strutil"
	"github.com/adrg/strutil/metrics"
)

type wordMatch struct {
	word  string
	score float64
}

type goSpell struct {
	dict map[string]struct{}

	ireplacer         *strings.Replacer
	compounds         []*regexp.Regexp
	splitter          *splitter
	affix             *dictConfig
	lazyMorphology    bool
	lazyBaseEntries   map[string][]lazyDictionaryEntry
	lazyMu            sync.Mutex
	lemmaMap          map[string]string
	baseToForms       map[string][]string
	baseToInflections map[string][]Inflection
}

type dictionary struct {
	dic string
	aff string
}

type lazyDictionaryEntry struct {
	line     string
	hasAffix bool
}

type goSpellLoadOptions struct {
	lazyMorphology  bool
	indexMorphology bool
}

func withUTF8Hint(err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%w; ensure the dictionary is UTF-8 encoded", err)
}

// inputConversion does any character substitution before checking
//
//	This is based on the ICONV stanza
func (s *goSpell) inputConversion(raw []byte) string {
	sraw := string(raw)
	if s.ireplacer == nil {
		return sraw
	}
	return s.ireplacer.Replace(sraw)
}

// addWordRaw adds a single word to the internal dictionary without modifications
// returns true if added
// return false is already exists
func (s *goSpell) addWordRaw(word string) bool {
	_, ok := s.dict[word]
	if ok {
		// already exists
		return false
	}
	s.dict[word] = struct{}{}
	return true
}

// addWordListFile reads in a word list file
func (s *goSpell) addWordListFile(name string) ([]string, error) {
	fd, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer fd.Close()
	return s.addWordList(fd)
}

// addWordList adds basic word lists, just one word per line
//
//	Assumed to be in UTF-8
//
// TODO: hunspell compatible with "*" prefix for forbidden words
// and affix support
// returns list of duplicated words and/or error
func (s *goSpell) addWordList(r io.Reader) ([]string, error) {
	var duplicates []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		word := strings.TrimSpace(scanner.Text())
		if len(word) == 0 || word == "#" {
			continue
		}
		if !s.addWordRaw(word) {
			duplicates = append(duplicates, word)
		}
	}
	if err := scanner.Err(); err != nil {
		return duplicates, err
	}
	return duplicates, nil
}

func (s *goSpell) keys() []string {
	keys := make([]string, len(s.dict))

	i := 0
	for k := range s.dict {
		keys[i] = k
		i++
	}

	return keys
}

func (s *goSpell) suggest(word string) []wordMatch {
	metric := metrics.NewLevenshtein()

	matches := []wordMatch{}
	for _, option := range s.keys() {
		sim := strutil.Similarity(option, word, metric)
		matches = append(matches, wordMatch{option, sim})
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})

	hits := matches[:5]
	if word == strings.Title(word) { //nolint:staticcheck
		// Capitalized word, so capitalize the suggestions
		for i := range hits {
			hits[i].word = strings.Title(hits[i].word) //nolint:staticcheck
		}
	}

	return hits
}

// spell checks to see if a given word is in the internal dictionaries
func (s *goSpell) spell(word string) bool {
	_, ok := s.dict[word]
	if ok {
		return true
	}
	_, ok = s.dict[strings.ToLower(word)]
	if ok {
		return true
	}

	if isNumber(word) {
		return true
	}
	if isNumberHex(word) {
		return true
	}

	if isNumberBinary(word) {
		return true
	}

	if isHash(word) {
		return true
	}

	// check compounds
	for _, pat := range s.compounds {
		if pat.MatchString(word) {
			return true
		}
	}

	// Maybe a word with units? e.g. 100GB
	units := isNumberUnits(word)
	if units != "" {
		// dictionary appears to have list of units
		if _, ok = s.dict[units]; ok {
			return true
		}
	}

	return false
}

// Expand returns all inflected forms of a given word based on the dictionary's
// affix rules. If the word is not found in the dictionary, it returns the
// word itself. If the word has no affix rules, it returns the word itself.
func (s *goSpell) Expand(word string) []string {
	inflections := s.ExpandWithLineage(word)
	forms := make([]string, 0, len(inflections))
	seen := make(map[string]struct{}, len(inflections))
	for _, inflection := range inflections {
		if _, ok := seen[inflection.Form]; ok {
			continue
		}
		seen[inflection.Form] = struct{}{}
		forms = append(forms, inflection.Form)
	}
	if len(forms) == 0 {
		return []string{word}
	}

	return forms
}

// ExpandWithLineage returns all inflected forms together with their affix
// derivation lineage.
func (s *goSpell) ExpandWithLineage(word string) []Inflection {
	if s.lazyMorphology {
		return s.expandWithLineageLazy(word)
	}

	return s.expandWithLineageEager(word)
}

func (s *goSpell) expandWithLineageEager(word string) []Inflection {
	if s.lemmaMap == nil {
		return []Inflection{{Form: word}}
	}

	base := s.lemmaMap[word]
	if base == "" {
		base = s.lemmaMap[strings.ToLower(word)]
	}
	if base == "" {
		base = word
	}

	if inflections, ok := s.baseToInflections[base]; ok {
		return inflections
	}
	if inflections, ok := s.baseToInflections[strings.ToLower(base)]; ok {
		return inflections
	}

	return []Inflection{{Form: word}}
}

func (s *goSpell) expandWithLineageLazy(word string) []Inflection {
	if s.affix == nil {
		return []Inflection{{Form: word}}
	}

	s.lazyMu.Lock()
	defer s.lazyMu.Unlock()

	base, ok := s.resolveLazyBase(word)
	if !ok {
		return []Inflection{{Form: word}}
	}

	if inflections, ok := s.baseToInflections[base]; ok {
		return inflections
	}

	entries, ok := s.lazyBaseEntries[base]
	if !ok {
		return []Inflection{{Form: word}}
	}

	inflections := []Inflection{}
	seen := map[string]struct{}{}
	for _, entry := range entries {
		derived, err := s.affix.expand(entry.line, nil)
		if err != nil {
			return []Inflection{{Form: word}}
		}

		for _, item := range derived {
			inflection := Inflection{
				Form:       item.word,
				Lineage:    item.lineage,
				LineageKey: item.lineageKey,
			}
			key := inflection.Form + "\x00" + inflection.Lineage
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			inflections = append(inflections, inflection)

			if entry.hasAffix {
				s.lemmaMap[item.word] = base
			} else if _, exists := s.lemmaMap[item.word]; !exists {
				s.lemmaMap[item.word] = base
			}
		}
	}

	if len(inflections) == 0 {
		inflections = []Inflection{{Form: base}}
	}

	s.baseToInflections[base] = inflections
	lower := strings.ToLower(base)
	if _, ok := s.baseToInflections[lower]; !ok {
		s.baseToInflections[lower] = inflections
	}
	return inflections
}

func (s *goSpell) resolveLazyBase(word string) (string, bool) {
	if base := s.lemmaMap[word]; base != "" {
		return base, true
	}

	lower := strings.ToLower(word)
	if base := s.lemmaMap[lower]; base != "" {
		return base, true
	}

	if _, ok := s.lazyBaseEntries[word]; ok {
		return word, true
	}
	return "", false
}

func mergeForms(existing []string, forms []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(forms))
	merged := make([]string, 0, len(existing)+len(forms))

	for _, form := range existing {
		if _, ok := seen[form]; ok {
			continue
		}
		seen[form] = struct{}{}
		merged = append(merged, form)
	}

	for _, form := range forms {
		if _, ok := seen[form]; ok {
			continue
		}
		seen[form] = struct{}{}
		merged = append(merged, form)
	}

	return merged
}

func mergeInflections(existing []Inflection, inflections []Inflection) []Inflection {
	seen := make(map[string]struct{}, len(existing)+len(inflections))
	merged := make([]Inflection, 0, len(existing)+len(inflections))

	for _, inflection := range existing {
		key := inflection.Form + "\x00" + inflection.Lineage
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, inflection)
	}

	for _, inflection := range inflections {
		key := inflection.Form + "\x00" + inflection.Lineage
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, inflection)
	}

	return merged
}

func cleanDictionaryLine(raw string) string {
	line := strings.TrimSpace(raw)
	if line == "" || isDictionaryComment(line) {
		return ""
	}

	if idx := strings.IndexRune(line, '\t'); idx >= 0 {
		line = line[:idx]
	}

	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}

	end := len(fields)
	for end > 1 && looksLikeMorphField(fields[end-1]) {
		end--
	}

	return strings.Join(fields[:end], " ")
}

func isDictionaryComment(line string) bool {
	return strings.HasPrefix(line, "#") ||
		line == "/" ||
		strings.HasPrefix(line, "/ ") ||
		strings.HasPrefix(line, "/\t")
}

func looksLikeMorphField(field string) bool {
	return strings.HasPrefix(field, "#") || strings.Contains(field, ":")
}

// newGoSpellReader creates a speller from io.Readers for
// Hunspell files
func newGoSpellReader(aff, dic io.Reader) (*goSpell, error) {
	return newGoSpellReaderWithOptions(aff, dic, goSpellLoadOptions{
		indexMorphology: true,
	})
}

func newGoSpellReaderWithOptions(aff, dic io.Reader, opts goSpellLoadOptions) (*goSpell, error) {
	affix, err := newDictConfig(aff)
	if err != nil {
		return nil, withUTF8Hint(err)
	}

	scanner := bufio.NewScanner(dic)
	// get first line
	if !scanner.Scan() {
		return nil, withUTF8Hint(scanner.Err())
	}
	if !utf8.ValidString(scanner.Text()) {
		return nil, withUTF8Hint(fmt.Errorf("dictionary data is not valid UTF-8"))
	}
	dictCap := 0
	if count, convErr := strconv.Atoi(strings.TrimSpace(scanner.Text())); convErr == nil && count > 0 {
		// The first DIC line is a rough lower bound for generated entries.
		dictCap = count
	}

	gs := goSpell{
		dict:           make(map[string]struct{}, dictCap),
		compounds:      make([]*regexp.Regexp, 0, len(affix.CompoundRule)),
		splitter:       newSplitter(affix.WordChars),
		affix:          affix,
		lazyMorphology: opts.lazyMorphology,
	}
	if opts.lazyMorphology {
		gs.lazyBaseEntries = make(map[string][]lazyDictionaryEntry)
		gs.lemmaMap = make(map[string]string)
		gs.baseToInflections = make(map[string][]Inflection)
	}
	if opts.indexMorphology {
		if gs.lemmaMap == nil {
			gs.lemmaMap = make(map[string]string)
		}
		gs.baseToForms = make(map[string][]string)
		if gs.baseToInflections == nil {
			gs.baseToInflections = make(map[string][]Inflection)
		}
	}

	derived := []derivedWord{}
	for scanner.Scan() {
		rawLine := scanner.Text()
		if !utf8.ValidString(rawLine) {
			return nil, withUTF8Hint(fmt.Errorf("dictionary data is not valid UTF-8"))
		}
		line := cleanDictionaryLine(rawLine)
		if line == "" {
			continue
		}

		if opts.lazyMorphology {
			baseWord, keyString, hasAffix, splitErr := affix.splitWordFlags(line)
			if splitErr != nil {
				return nil, withUTF8Hint(fmt.Errorf("unable to process %q: %w", line, splitErr))
			}

			gs.dict[baseWord] = struct{}{}
			if _, ok := gs.lemmaMap[baseWord]; !ok {
				gs.lemmaMap[baseWord] = baseWord
			}
			gs.lazyBaseEntries[baseWord] = append(gs.lazyBaseEntries[baseWord], lazyDictionaryEntry{
				line:     line,
				hasAffix: hasAffix,
			})

			if hasAffix {
				keys, keyErr := affix.resolveDictionaryFlags(keyString)
				if keyErr != nil {
					return nil, withUTF8Hint(fmt.Errorf("unable to process %q: %w", line, keyErr))
				}

				for _, key := range keys {
					if _, ok := affix.CompoundOnly[key]; ok {
						continue
					}
					if _, ok := affix.compoundMap[key]; !ok {
						continue
					}
					affix.compoundMap[key] = append(affix.compoundMap[key], baseWord)
				}
			}

			continue
		}

		baseWord := ""
		hasAffix := false
		if opts.indexMorphology {
			var splitErr error
			baseWord, _, hasAffix, splitErr = affix.splitWordFlags(line)
			if splitErr != nil {
				return nil, withUTF8Hint(fmt.Errorf("unable to process %q: %w", line, splitErr))
			}
		}

		if !opts.indexMorphology {
			derived, err = affix.expandWithoutLineage(line, derived)
			if err != nil {
				return nil, withUTF8Hint(fmt.Errorf("unable to process %q: %s", line, err.Error()))
			}

			if len(derived) == 0 {
				continue
			}
			for _, item := range derived {
				gs.dict[item.word] = struct{}{}
			}
			continue
		}
		derived, err = affix.expand(line, derived)
		if err != nil {
			return nil, withUTF8Hint(fmt.Errorf("unable to process %q: %s", line, err.Error()))
		}

		if len(derived) == 0 {
			continue
		}

		words := make([]string, 0, len(derived))
		inflections := make([]Inflection, 0, len(derived))
		for _, item := range derived {
			words = append(words, item.word)
			inflections = append(inflections, Inflection{
				Form:       item.word,
				Lineage:    item.lineage,
				LineageKey: item.lineageKey,
			})

			gs.dict[item.word] = struct{}{}
			if hasAffix {
				gs.lemmaMap[item.word] = baseWord
			} else if _, ok := gs.lemmaMap[item.word]; !ok {
				gs.lemmaMap[item.word] = baseWord
			}
		}

		gs.baseToForms[baseWord] = mergeForms(gs.baseToForms[baseWord], words)
		gs.baseToInflections[baseWord] = mergeInflections(gs.baseToInflections[baseWord], inflections)
	}

	if err = scanner.Err(); err != nil {
		return nil, withUTF8Hint(err)
	}

	for _, compoundRule := range affix.CompoundRule {
		pat, compileErr := compileCompoundPattern(affix, compoundRule)
		if compileErr != nil {
			return nil, compileErr
		}
		gs.compounds = append(gs.compounds, pat)
	}

	if len(affix.IconvReplacements) > 0 {
		gs.ireplacer = strings.NewReplacer(affix.IconvReplacements...)
	}
	return &gs, nil
}

// newGoSpell from AFF and DIC Hunspell filenames
func newGoSpell(affFile, dicFile string) (*goSpell, error) {
	return newGoSpellWithOptions(affFile, dicFile, goSpellLoadOptions{
		indexMorphology: true,
	})
}

func newGoSpellWithOptions(affFile, dicFile string, opts goSpellLoadOptions) (*goSpell, error) {
	aff, err := os.Open(affFile)
	if err != nil {
		return nil, fmt.Errorf("unable to open aff: %s", err.Error())
	}
	defer aff.Close()
	dic, err := os.Open(dicFile)
	if err != nil {
		return nil, fmt.Errorf("unable to open dic: %s", err.Error())
	}
	defer dic.Close()
	h, err := newGoSpellReaderWithOptions(aff, dic, opts)
	return h, err
}

func compileCompoundPattern(affix *dictConfig, compoundRule string) (*regexp.Regexp, error) {
	tokens, tokenErr := affix.tokenizeCompoundRule(compoundRule)
	if tokenErr != nil {
		tokens = make([]compoundToken, 0, len(compoundRule))
		for _, r := range compoundRule {
			if isCompoundOperator(r) {
				tokens = append(tokens, compoundToken{lit: string(r)})
				continue
			}
			tokens = append(tokens, compoundToken{flag: string(r), isFlag: true})
		}
	}

	pattern := "^"
	for _, token := range tokens {
		if token.isFlag {
			groups := affix.compoundMap[token.flag]
			pattern += "(" + strings.Join(groups, "|") + ")"
			continue
		}
		pattern += regexp.QuoteMeta(token.lit)
	}
	pattern += "$"

	return regexp.Compile(pattern)
}
