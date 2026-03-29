package check

import (
	"fmt"
	"sort"
	"strings"

	"golang.org/x/exp/maps"

	"github.com/errata-ai/regexp2"
	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/nlp"
	"github.com/errata-ai/vale/v3/internal/spell"
)

type morphologyReplacement struct {
	partMaps []map[string]string
}

// Substitution switches the values of Swap for its keys.
type Substitution struct {
	Definition   `mapstructure:",squash"`
	Exceptions   []string
	repl         []string
	Swap         map[string]string
	exceptRe     *regexp2.Regexp
	pattern      *regexp2.Regexp
	Ignorecase   bool
	Nonword      bool
	Vocab        bool
	Capitalize   bool
	Morphology   bool
	Dictionaries []string
	Aff          string
	Dic          string
	Dicpath      string
	Append       bool

	msgMap   []string
	morphMap []morphologyReplacement
	path     string
	// Deprecated
	POS string
}

// NewSubstitution creates a new `substitution`-based rule.
func NewSubstitution(cfg *core.Config, generic baseCheck, path string) (*Substitution, error) {
	rule := &Substitution{Vocab: true, path: path}

	err := decodeRule(generic, rule)
	if err != nil {
		return rule, readStructureError(err, path)
	}

	err = checkScopes(rule.Scope, path)
	if err != nil {
		return rule, err
	}

	re, err := updateExceptions(rule.Exceptions, cfg.AcceptedTokens, rule.Vocab)
	if err != nil {
		return rule, core.NewE201FromPosition(err.Error(), path, 1)
	}
	rule.exceptRe = re

	terms := maps.Keys(rule.Swap)
	sort.Slice(terms, func(p, q int) bool {
		return len(terms[p]) > len(terms[q])
	})

	for _, regexstr := range terms {
		rule.msgMap = append(rule.msgMap, regexstr)
		rule.repl = append(rule.repl, rule.Swap[regexstr])
	}

	re, err = rule.compilePattern(cfg)
	if err != nil {
		return rule, err
	}

	rule.pattern = re

	return rule, nil
}

// expandForMorphology expands a word to all its inflected forms using the
// dictionary. If the word has no morphological variations, it returns the
// word itself.
func expandForMorphology(word string, gs *spell.Checker) string {
	if gs == nil {
		return word
	}

	parts := strings.Split(word, " ")
	if len(parts) > 1 {
		result := []string{}
		expanded := false
		for _, p := range parts {
			pForms := gs.Expand(p)
			if len(pForms) > 1 {
				result = append(result, toAlternation(pForms))
				expanded = true
			} else {
				result = append(result, p)
			}
		}
		if expanded {
			return strings.Join(result, " ")
		}
		return word
	}

	forms := gs.Expand(word)
	if len(forms) <= 1 {
		return word
	}

	return toAlternation(forms)
}

func toAlternation(forms []string) string {
	return "(?:" + strings.Join(forms, "|") + ")"
}

func cloneStrings(values []string) []string {
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

func buildMorphologyReplacement(source, replacement string, checker *spell.Checker) (morphologyReplacement, bool) {
	if checker == nil {
		return morphologyReplacement{}, false
	}

	sourceParts := strings.Split(source, " ")
	replacementParts := strings.Split(replacement, " ")
	if len(sourceParts) == 0 || len(sourceParts) != len(replacementParts) {
		return morphologyReplacement{}, false
	}

	maps := make([]map[string]string, len(sourceParts))
	for i := range sourceParts {
		srcPart := sourceParts[i]
		replPart := replacementParts[i]

		partMap := map[string]string{strings.ToLower(srcPart): replPart}
		sourceInflections := checker.ExpandWithLineage(srcPart)
		replacementInflections := checker.ExpandWithLineage(replPart)

		replacementByLineage := map[string]string{}
		replacementByLineageKey := map[string]string{}
		ambiguousLineageKeys := map[string]struct{}{}
		for _, inflection := range replacementInflections {
			if inflection.Lineage == "" {
				if inflection.LineageKey == "" {
					continue
				}
			} else if _, ok := replacementByLineage[inflection.Lineage]; !ok {
				replacementByLineage[inflection.Lineage] = inflection.Form
			}

			if inflection.LineageKey == "" {
				continue
			}
			if _, ambiguous := ambiguousLineageKeys[inflection.LineageKey]; ambiguous {
				continue
			}
			if existing, ok := replacementByLineageKey[inflection.LineageKey]; ok && existing != inflection.Form {
				delete(replacementByLineageKey, inflection.LineageKey)
				ambiguousLineageKeys[inflection.LineageKey] = struct{}{}
				continue
			}
			replacementByLineageKey[inflection.LineageKey] = inflection.Form
		}

		for _, inflection := range sourceInflections {
			key := strings.ToLower(inflection.Form)
			if _, exists := partMap[key]; exists {
				continue
			}

			if inflection.Lineage != "" {
				if mapped, ok := replacementByLineage[inflection.Lineage]; ok {
					partMap[key] = mapped
					continue
				}
			}
			if inflection.LineageKey != "" {
				if _, ambiguous := ambiguousLineageKeys[inflection.LineageKey]; !ambiguous {
					if mapped, ok := replacementByLineageKey[inflection.LineageKey]; ok {
						partMap[key] = mapped
						continue
					}
				}
			}

			partMap[key] = replPart
		}

		maps[i] = partMap
	}

	return morphologyReplacement{partMaps: maps}, true
}

func (m morphologyReplacement) replacementForObserved(observed string) (string, bool) {
	if len(m.partMaps) == 0 {
		return "", false
	}

	parts := strings.Split(observed, " ")
	if len(parts) != len(m.partMaps) {
		return "", false
	}

	replaced := make([]string, len(parts))
	for i, part := range parts {
		repl, ok := m.partMaps[i][strings.ToLower(part)]
		if !ok {
			return "", false
		}
		replaced[i] = repl
	}

	return strings.Join(replaced, " "), true
}

func morphologyCheckerKey(
	cfg *core.Config,
	rulePath,
	aff,
	dic,
	dicpath string,
	appendDefault bool,
	dictionaries []string,
) string {
	builder := strings.Builder{}
	builder.Grow(len(cfg.StylesPath()) + len(rulePath) + len(aff) + len(dic) + len(dicpath) + len(dictionaries)*12 + 8)
	builder.WriteString(cfg.StylesPath())
	builder.WriteByte('\x00')
	builder.WriteString(rulePath)
	builder.WriteByte('\x00')
	builder.WriteString(aff)
	builder.WriteByte('\x00')
	builder.WriteString(dic)
	builder.WriteByte('\x00')
	builder.WriteString(dicpath)
	builder.WriteByte('\x00')
	if appendDefault {
		builder.WriteByte('1')
	} else {
		builder.WriteByte('0')
	}

	for _, name := range dictionaries {
		builder.WriteByte('\x00')
		builder.WriteString(name)
	}

	return builder.String()
}

func (s *Substitution) makeMorphologyChecker(cfg *core.Config) (*spell.Checker, error) {
	if !s.Morphology {
		return nil, nil
	}

	dictionaries := cloneStrings(s.Dictionaries)
	cacheKey := morphologyCheckerKey(
		cfg,
		s.path,
		s.Aff,
		s.Dic,
		s.Dicpath,
		s.Append,
		dictionaries,
	)
	return loadMorphologyChecker(cacheKey, func() (*spell.Checker, error) {
		return makeSpeller(&Spelling{
			Aff:          s.Aff,
			Dic:          s.Dic,
			Dicpath:      s.Dicpath,
			Dictionaries: dictionaries,
			Append:       s.Append,
			lazyMorph:    true,
		}, cfg, s.path)
	})
}

func (s *Substitution) compilePattern(cfg *core.Config) (*regexp2.Regexp, error) {
	regex := makeRegexp(
		cfg.WordTemplate,
		s.Ignorecase,
		func() bool { return !s.Nonword },
		func() string { return "" }, true)

	checker, err := s.makeMorphologyChecker(cfg)
	if err != nil {
		return nil, err
	}

	s.morphMap = make([]morphologyReplacement, len(s.msgMap))

	tokens := ""
	for i, regexstr := range s.msgMap {
		expanded := regexstr
		if checker != nil {
			if mapped, ok := buildMorphologyReplacement(regexstr, s.repl[i], checker); ok {
				s.morphMap[i] = mapped
			}
			expanded = expandForMorphology(regexstr, checker)
		}

		opens := strings.Count(expanded, "(")
		if opens != strings.Count(expanded, "(?")+strings.Count(expanded, `\(`) {
			expanded, err = convertCaptureGroups(expanded)
			if err != nil {
				return nil, core.NewE201FromTarget(err.Error(), expanded, s.path)
			}
		}
		tokens += `(` + expanded + `)|`
	}

	regex = fmt.Sprintf(regex, strings.TrimRight(tokens, "|"))

	pattern, err := regexp2.CompileStd(regex)
	if err != nil {
		return nil, core.NewE201FromPosition(err.Error(), s.path, 1)
	}

	return pattern, nil
}

// Run executes the `substitution`-based rule.
//
// The rule looks for one pattern and then suggests a replacement.
func (s *Substitution) Run(blk nlp.Block, _ *core.File, cfg *core.Config) ([]core.Alert, error) {
	var alerts []core.Alert

	txt := blk.Text
	// Leave early if we can to avoid calling `FindAllStringSubmatchIndex`
	// unnecessarily.
	if !s.pattern.MatchStringStd(txt) {
		return alerts, nil
	}

	for _, submat := range s.pattern.FindAllStringSubmatchIndex(txt, -1) {
		for idx, mat := range submat {
			if mat != -1 && idx > 0 && idx%2 == 0 {
				loc := []int{mat, submat[idx+1]}

				converted, convErr := re2Loc(txt, loc)
				if convErr != nil {
					return alerts, convErr
				}

				observed := converted
				expected, msgErr := subMsg(s, (idx/2)-1, observed)
				if msgErr != nil {
					return alerts, msgErr
				}

				same := matchToken(expected, observed, false)
				if !same && !isMatch(s.exceptRe, observed) {
					action := s.Fields().Action
					message := s.Message
					if action.Name == "replace" && len(action.Params) == 0 {
						action.Params = getOptions(expected)
						if s.Capitalize && observed == core.CapFirst(observed) {
							cased := []string{}
							for _, param := range action.Params {
								cased = append(cased, core.CapFirst(param))
							}
							action.Params = cased
						}

						expected = core.ToSentence(action.Params, "or")
						// NOTE: For backwards-compatibility, we need to ensure
						// that we don't double quote.
						message = convertMessage(message)
					}

					a, aerr := makeAlert(s.Definition, loc, txt, cfg)
					if aerr != nil {
						return alerts, aerr
					}

					a.Message, a.Description = formatMessages(message,
						s.Description, expected, observed)
					a.Action = action

					alerts = append(alerts, a)
				}
			}
		}
	}

	return alerts, nil
}

// Fields provides access to the internal rule definition.
func (s *Substitution) Fields() Definition {
	return s.Definition
}

// Pattern is the internal regex pattern used by this rule.
func (s *Substitution) Pattern() string {
	if s.pattern == nil {
		return ""
	}
	return s.pattern.String()
}

func convertMessage(s string) string {
	for _, spec := range []string{"'%s'", "\"%s\""} {
		if strings.Count(s, spec) == 2 {
			s = strings.Replace(s, spec, "%s", 1)
		}
	}
	return s
}

func convertCaptureGroups(msg string) (string, error) {
	captureOpen := regexp2.MustCompileStd(`(?<!\\)\((?!\?)`)
	return captureOpen.Replace(msg, "(?:", -1, -1)
}

func subMsg(s *Substitution, index int, observed string) (string, error) {
	// Based on the current capture group (`idx`), we can determine
	// the associated replacement string by using the `repl` slice:
	expected := s.repl[index]
	if index >= 0 && index < len(s.morphMap) {
		if inflected, ok := s.morphMap[index].replacementForObserved(observed); ok {
			expected = inflected
		}
	}

	if s.Capitalize && observed == core.CapFirst(observed) {
		expected = core.CapFirst(expected)
	}

	// TODO: Why do we need to check for this?
	//
	// This feels like a bug in `regexp2`.
	hasIndex := regexp2.MustCompileStd(`\$\d+`)
	if !hasIndex.MatchStringStd(expected) {
		return expected, nil
	}

	msg := s.msgMap[index]
	if s.Ignorecase {
		msg = `(?i)` + msg
	}

	msgRe := regexp2.MustCompileStd(msg)
	return msgRe.Replace(observed, expected, -1, -1)
}

// getOptions returns a slice of options from a match.
//
// For example, given the match "a|b|c", this function will return
// []string{"a", "b", "c"}.
//
// This allows the user to specify multiple options for a single match.
//
// https://vale.sh/docs/checks/substitution#multiple-suggestions
func getOptions(match string) []string {
	options := []string{}

	// We want to ignore any escaped pipes, so make a temporary substitution:
	//
	// TODO: Add support for `.Split` in `regexp2`.
	temp := strings.ReplaceAll(match, `\|`, "PIPE")

	for _, option := range strings.Split(temp, "|") {
		if option != "" {
			options = append(options, strings.ReplaceAll(option, "PIPE", `|`))
		}
	}

	return options
}
