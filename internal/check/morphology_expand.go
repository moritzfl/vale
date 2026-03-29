package check

import (
	"strings"

	"github.com/errata-ai/vale/v3/internal/spell"
)

const morphologyRegexGroupPrefix = "morph_"

// expandForMorphology expands a word/token to all its inflected forms using
// the dictionary. If the token has no morphological variations, it returns the
// token itself.
func expandForMorphology(token string, checker *spell.Checker) string {
	if checker == nil {
		return token
	}
	if expanded, ok := expandMarkedMorphGroups(token, checker); ok {
		return expanded
	}

	if options, ok := splitMorphAlternatives(token); ok && len(options) > 1 {
		forms := []string{}
		seen := map[string]struct{}{}
		expanded := false

		for _, option := range options {
			optionForms := checker.Expand(option)
			if len(optionForms) > 1 {
				expanded = true
			}
			if len(optionForms) == 0 {
				optionForms = []string{option}
			}

			for _, form := range optionForms {
				if _, exists := seen[form]; exists {
					continue
				}
				seen[form] = struct{}{}
				forms = append(forms, form)
			}
		}

		if expanded {
			return toAlternation(forms)
		}

		return token
	}

	parts := strings.Split(token, " ")
	if len(parts) > 1 {
		result := make([]string, 0, len(parts))
		expanded := false
		for _, part := range parts {
			partForms := checker.Expand(part)
			if len(partForms) > 1 {
				result = append(result, toAlternation(partForms))
				expanded = true
			} else {
				result = append(result, part)
			}
		}
		if expanded {
			return strings.Join(result, " ")
		}
		return token
	}

	forms := checker.Expand(token)
	if len(forms) <= 1 {
		return token
	}

	return toAlternation(forms)
}

func toAlternation(forms []string) string {
	return "(?:" + strings.Join(forms, "|") + ")"
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
	if strings.Contains(pattern, `\|`) || strings.ContainsAny(pattern, `()[]{}*+?^$\`) {
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
		if strings.ContainsAny(option, `()[]{}*+?^$\`) {
			return nil, false
		}
	}

	return options, true
}

func expandMarkedMorphGroups(pattern string, checker *spell.Checker) (string, bool) {
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
			builder.WriteString(expandForMorphology(groupBody, checker))
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
