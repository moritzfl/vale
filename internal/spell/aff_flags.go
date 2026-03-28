package spell

import (
	"fmt"
	"strings"
)

func splitWordFlags(wordAffix string) (string, string, bool, error) {
	idx := strings.Index(wordAffix, "/")
	if idx == -1 {
		return wordAffix, "", false, nil
	}
	if idx == 0 || idx+1 == len(wordAffix) {
		return "", "", false, fmt.Errorf("slash char found in first or last position")
	}

	return wordAffix[:idx], wordAffix[idx+1:], true, nil
}

// parseSingleFlag keeps the current single-byte flag behavior in one place.
func parseSingleFlag(raw string) (rune, error) {
	if raw == "" {
		return 0, fmt.Errorf("expected flag")
	}

	return rune(raw[0]), nil
}

func flagRunes(raw string) []rune {
	return []rune(raw)
}

func isCompoundOnlyFlag(compoundOnly string, flag rune) bool {
	return strings.ContainsRune(compoundOnly, flag)
}

func registerCompoundRuleFlags(rule string, compoundMap map[rune][]string) {
	for _, flag := range rule {
		if _, ok := compoundMap[flag]; ok {
			continue
		}
		compoundMap[flag] = []string{}
	}
}
