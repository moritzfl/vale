package check

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/errata-ai/regexp2"
	"github.com/errata-ai/vale/v3/internal/spell"
)

const morphologyRegexGroupPrefix = "morph_"

type morphSplit struct {
	words      []string
	separators []string
}

var morphologyWordMatcherCache sync.Map

// expandForMorphology expands a word/token to all its inflected forms using
// the dictionary. If the token has no morphological variations, it returns the
// token itself.
func expandForMorphology(token string, checker *spell.Checker, template string) string {
	if checker == nil {
		return token
	}
	if expanded, ok := expandMarkedMorphGroups(token, checker, template); ok {
		return expanded
	}

	if options, ok := splitMorphAlternatives(token); ok && len(options) > 1 {
		forms := []string{}
		seen := map[string]struct{}{}
		expanded := false

		for _, option := range options {
			expandedOption := expandForMorphology(option, checker, template)
			if expandedOption != option {
				expanded = true
			}
			if _, exists := seen[expandedOption]; exists {
				continue
			}
			seen[expandedOption] = struct{}{}
			forms = append(forms, expandedOption)
		}

		if expanded {
			return toAlternation(forms)
		}

		return token
	}

	split := splitForMorphology(token, template, !containsRegexSyntax(token))
	if len(split.words) > 1 {
		result := make([]string, 0, len(split.words))
		expanded := false
		for _, part := range split.words {
			partForms := checker.Expand(part)
			if len(partForms) > 1 {
				result = append(result, toLiteralAlternation(partForms))
				expanded = true
			} else {
				result = append(result, part)
			}
		}
		if expanded {
			return joinWithSeparators(result, split.separators)
		}
		return token
	}

	forms := checker.Expand(token)
	if len(forms) <= 1 {
		return token
	}

	return toLiteralAlternation(forms)
}

func toAlternation(forms []string) string {
	return "(?:" + strings.Join(forms, "|") + ")"
}

func toLiteralAlternation(forms []string) string {
	escaped := make([]string, len(forms))
	for i, form := range forms {
		escaped[i] = regexp.QuoteMeta(form)
	}

	return toAlternation(escaped)
}

func splitEscapedAlternatives(pattern string) []string {
	options := []string{}

	// Ignore escaped pipes while splitting.
	temp := strings.ReplaceAll(pattern, `\|`, "PIPE")
	for _, option := range strings.Split(temp, "|") {
		if option == "" {
			continue
		}
		options = append(options, strings.ReplaceAll(option, "PIPE", `|`))
	}

	return options
}

func splitMorphAlternatives(pattern string) ([]string, bool) {
	if !strings.Contains(pattern, "|") {
		return []string{pattern}, true
	}
	// Keep full regex patterns untouched. We only expand literal alternations.
	if strings.Contains(pattern, `\|`) || containsRegexSyntax(pattern) {
		return nil, false
	}

	options := splitEscapedAlternatives(pattern)
	if len(options) <= 1 {
		return nil, false
	}
	for _, option := range options {
		if option == "" {
			return nil, false
		}
		if containsRegexSyntax(option) {
			return nil, false
		}
	}

	return options, true
}

func expandMarkedMorphGroups(pattern string, checker *spell.Checker, template string) (string, bool) {
	seenMarker := false
	var builder strings.Builder

	for i := 0; i < len(pattern); {
		groupName, groupBody, groupEnd, ok := parseNamedCaptureGroup(pattern, i)
		if !ok {
			builder.WriteByte(pattern[i])
			i++
			continue
		}

		if strings.HasPrefix(groupName, morphologyRegexGroupPrefix) {
			seenMarker = true
			builder.WriteString("(?:")
			builder.WriteString(expandForMorphology(groupBody, checker, template))
			builder.WriteString(")")
		} else {
			builder.WriteString(pattern[i : groupEnd+1])
		}
		i = groupEnd + 1
	}

	if !seenMarker {
		return pattern, false
	}
	return builder.String(), true
}

func unwrapMarkedMorphGroup(pattern string) (string, bool) {
	groupName, groupBody, groupEnd, ok := parseNamedCaptureGroup(pattern, 0)
	if !ok || groupEnd != len(pattern)-1 {
		return "", false
	}
	if !strings.HasPrefix(groupName, morphologyRegexGroupPrefix) {
		return "", false
	}
	return groupBody, true
}

func parseNamedCaptureGroup(pattern string, start int) (string, string, int, bool) {
	if start < 0 || start+4 >= len(pattern) {
		return "", "", -1, false
	}
	if !strings.HasPrefix(pattern[start:], "(?<") {
		return "", "", -1, false
	}
	if pattern[start+3] == '=' || pattern[start+3] == '!' {
		return "", "", -1, false
	}

	nameStart := start + 3
	nameEnd := strings.IndexByte(pattern[nameStart:], '>')
	if nameEnd < 0 {
		return "", "", -1, false
	}
	nameEnd += nameStart

	groupEnd, ok := findRegexGroupEnd(pattern, start)
	if !ok || groupEnd <= nameEnd {
		return "", "", -1, false
	}

	return pattern[nameStart:nameEnd], pattern[nameEnd+1 : groupEnd], groupEnd, true
}

func findRegexGroupEnd(pattern string, start int) (int, bool) {
	if start < 0 || start >= len(pattern) || pattern[start] != '(' {
		return -1, false
	}

	depth := 0
	escaped := false
	for i := start; i < len(pattern); i++ {
		ch := pattern[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}

		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, true
			}
			if depth < 0 {
				return -1, false
			}
		}
	}

	return -1, false
}

func containsRegexSyntax(s string) bool {
	return strings.ContainsAny(s, `()[]{}*+?^$\`)
}

func splitForMorphology(input, template string, useWordTemplate bool) morphSplit {
	if input == "" {
		return morphSplit{words: []string{""}, separators: []string{"", ""}}
	}

	if !strings.ContainsAny(input, " \t\n\r\f\v") {
		return morphSplit{words: []string{input}, separators: []string{"", ""}}
	}

	if !useWordTemplate {
		return morphSplit{words: []string{input}, separators: []string{"", ""}}
	}

	if split, ok := splitByWordTemplate(input, template); ok && len(split.words) > 1 {
		return split
	}

	// No fallback to whitespace splitting: if WordTemplate can't segment this
	// safely, treat the full input as a single token.
	return morphSplit{words: []string{input}, separators: []string{"", ""}}
}

func splitByWordTemplate(input, template string) (morphSplit, bool) {
	re := getMorphWordMatcher(template)
	if re == nil {
		return morphSplit{}, false
	}

	locs := re.FindAllStringIndex(input, -1)
	if len(locs) == 0 {
		return morphSplit{}, false
	}

	runes := []rune(input)
	words := make([]string, 0, len(locs))
	separators := make([]string, 0, len(locs)+1)
	prev := 0

	for _, loc := range locs {
		if len(loc) != 2 || loc[0] < prev || loc[1] < loc[0] || loc[1] > len(runes) {
			return morphSplit{}, false
		}

		separators = append(separators, string(runes[prev:loc[0]]))
		words = append(words, string(runes[loc[0]:loc[1]]))
		prev = loc[1]
	}

	separators = append(separators, string(runes[prev:]))
	return morphSplit{words: words, separators: separators}, true
}

func getMorphWordMatcher(template string) *regexp2.Regexp {
	if cached, ok := morphologyWordMatcherCache.Load(template); ok {
		return cached.(*regexp2.Regexp)
	}

	pattern := makeRegexp(
		template,
		false,
		func() bool { return true },
		func() string { return "" },
		true,
	)
	if !strings.Contains(pattern, "%s") {
		var empty *regexp2.Regexp
		morphologyWordMatcherCache.Store(template, empty)
		return nil
	}

	compiled, err := regexp2.CompileStd(fmt.Sprintf(pattern, `\S+`))
	if err != nil {
		var empty *regexp2.Regexp
		morphologyWordMatcherCache.Store(template, empty)
		return nil
	}

	morphologyWordMatcherCache.Store(template, compiled)
	return compiled
}

func joinWithSeparators(words, separators []string) string {
	if len(words) == 0 {
		return ""
	}
	if len(separators) != len(words)+1 {
		return strings.Join(words, " ")
	}

	var builder strings.Builder
	for i, word := range words {
		builder.WriteString(separators[i])
		builder.WriteString(word)
	}
	builder.WriteString(separators[len(separators)-1])

	return builder.String()
}
