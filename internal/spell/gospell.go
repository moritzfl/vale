package spell

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
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
	lemmaMap          map[string]string
	baseToForms       map[string][]string
	baseToInflections map[string][]Inflection
}

type dictionary struct {
	dic string
	aff string
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

func readUTF8(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("dictionary data is not valid UTF-8")
	}

	return data, nil
}

// newGoSpellReader creates a speller from io.Readers for
// Hunspell files
func newGoSpellReader(aff, dic io.Reader) (*goSpell, error) {
	affData, err := readUTF8(aff)
	if err != nil {
		return nil, withUTF8Hint(err)
	}

	affix, err := newDictConfig(bytes.NewReader(affData))
	if err != nil {
		return nil, withUTF8Hint(err)
	}

	dicData, err := readUTF8(dic)
	if err != nil {
		return nil, withUTF8Hint(err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(dicData))
	// get first line
	if !scanner.Scan() {
		return nil, withUTF8Hint(scanner.Err())
	}

	gs := goSpell{
		// TODO: Use fixed size from first list?
		dict:              make(map[string]struct{}),
		compounds:         make([]*regexp.Regexp, 0, len(affix.CompoundRule)),
		splitter:          newSplitter(affix.WordChars),
		lemmaMap:          make(map[string]string),
		baseToForms:       make(map[string][]string),
		baseToInflections: make(map[string][]Inflection),
	}

	derived := []derivedWord{}
	for scanner.Scan() {
		line := scanner.Text()
		// NOTE: We do this for entries like
		//
		// abandonware/M	Noun: uncountable
		line = strings.Split(line, "\t")[0]

		baseWord, _, hasAffix, splitErr := splitWordFlags(line)
		if splitErr != nil {
			return nil, withUTF8Hint(fmt.Errorf("unable to process %q: %w", line, splitErr))
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

		pat, perr := regexp.Compile(pattern)
		if perr != nil {
			return nil, perr
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
	h, err := newGoSpellReader(aff, dic)
	return h, err
}
