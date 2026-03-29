package spell

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

const zeroWidthJoiner = '\u200d'

func (a dictConfig) splitWordFlags(entry string) (string, string, bool, error) {
	slashes := unescapedSlashes(entry)
	if len(slashes) == 0 {
		return strings.ReplaceAll(entry, `\/`, "/"), "", false, nil
	}

	for i := len(slashes) - 1; i >= 0; i-- {
		slash := slashes[i]
		if slash == 0 {
			flagSpec := entry[1:]
			if a.isDictionaryFlagSpec(flagSpec) {
				return "/", flagSpec, true, nil
			}
			continue
		}
		if slash == len(entry)-1 {
			return strings.ReplaceAll(entry[:slash], `\/`, "/"), "", false, nil
		}

		flagSpec := entry[slash+1:]
		if !a.isDictionaryFlagSpec(flagSpec) {
			continue
		}

		word := strings.ReplaceAll(entry[:slash], `\/`, "/")
		return word, flagSpec, true, nil
	}

	if slashes[0] == 0 {
		return strings.ReplaceAll(entry, `\/`, "/"), "", false, nil
	}

	return strings.ReplaceAll(entry, `\/`, "/"), "", false, nil
}

func unescapedSlashes(entry string) []int {
	slashes := make([]int, 0, 1)
	escaped := false
	for i, r := range entry {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '/' {
			slashes = append(slashes, i)
		}
	}

	return slashes
}

func (a dictConfig) isDictionaryFlagSpec(raw string) bool {
	if raw == "" {
		return false
	}

	if flags, ok := a.resolveAliasFlags(raw); ok && len(flags) > 0 {
		return true
	}

	_, err := a.parseFlags(raw)
	return err == nil
}

func parseFlagMode(raw string) (flagMode, string, error) {
	switch strings.ToLower(raw) {
	case "", "ascii":
		return flagASCII, "ASCII", nil
	case "utf-8", "utf8":
		return flagUTF8, "UTF-8", nil
	case "long":
		return flagLong, "long", nil
	case "num":
		return flagNum, "num", nil
	default:
		return flagASCII, "", fmt.Errorf("unsupported FLAG mode %q", raw)
	}
}

func (a dictConfig) parseFlags(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}

	switch a.flagMode {
	case flagASCII:
		flags := make([]string, 0, len(raw))
		for _, r := range raw {
			flags = append(flags, string(r))
		}
		return flags, nil
	case flagUTF8:
		return splitUTF8Flags(raw), nil
	case flagLong:
		runes := []rune(raw)
		if len(runes)%2 != 0 {
			return nil, fmt.Errorf("long flag %q must contain an even number of runes", raw)
		}

		flags := make([]string, 0, len(runes)/2)
		for i := 0; i < len(runes); i += 2 {
			flags = append(flags, string(runes[i:i+2]))
		}
		return flags, nil
	case flagNum:
		parts := strings.Split(raw, ",")
		flags := make([]string, 0, len(parts))
		for _, part := range parts {
			flag := strings.TrimSpace(part)
			if flag == "" {
				continue
			}
			for _, r := range flag {
				if !unicode.IsDigit(r) {
					return nil, fmt.Errorf("num flag %q contains non-digit %q", flag, string(r))
				}
			}
			flags = append(flags, flag)
		}
		return flags, nil
	default:
		return nil, fmt.Errorf("unknown flag mode: %d", a.flagMode)
	}
}

func (a dictConfig) parseSingleFlag(raw string) (string, error) {
	flags, err := a.parseFlags(raw)
	if err != nil {
		return "", err
	}
	if len(flags) != 1 {
		return "", fmt.Errorf("expected exactly one flag, got %q", raw)
	}

	return flags[0], nil
}

func isAliasRef(raw string) bool {
	for _, r := range raw {
		if unicode.IsDigit(r) || r == ',' || unicode.IsSpace(r) {
			continue
		}
		return false
	}
	return true
}

func parseAliasIndexes(raw string) ([]int, error) {
	parts := strings.Split(raw, ",")
	indexes := make([]int, 0, len(parts))
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		i, err := strconv.Atoi(p)
		if err != nil {
			return nil, err
		}
		indexes = append(indexes, i)
	}
	return indexes, nil
}

func (a dictConfig) resolveAliasFlags(raw string) ([]string, bool) {
	if len(a.flagAliases) == 0 || !isAliasRef(raw) {
		return nil, false
	}

	indexes, err := parseAliasIndexes(raw)
	if err != nil || len(indexes) == 0 {
		return nil, false
	}

	expanded := make([]string, 0, len(indexes)*2)
	for _, idx := range indexes {
		if idx <= 0 || idx > len(a.flagAliases) {
			return nil, false
		}
		expanded = append(expanded, a.flagAliases[idx-1]...)
	}

	return expanded, true
}

func (a dictConfig) resolveDictionaryFlags(raw string) ([]string, error) {
	if flags, ok := a.resolveAliasFlags(raw); ok {
		return flags, nil
	}

	return a.parseFlags(raw)
}

func (a dictConfig) splitAffixAndContinuation(raw string) (string, []string, error) {
	if raw == "0" {
		return "", nil, nil
	}

	split := strings.Index(raw, "/")
	if split == -1 {
		return raw, nil, nil
	}

	affixText := raw[:split]
	if affixText == "0" {
		affixText = ""
	}

	continuationRaw := raw[split+1:]
	continuation, err := a.resolveDictionaryFlags(continuationRaw)
	if err != nil {
		return "", nil, err
	}

	return affixText, continuation, nil
}

func isCompoundOperator(r rune) bool {
	switch r {
	case '(', ')', '+', '?', '*':
		return true
	default:
		return false
	}
}

// Real Hunspell UTF-8 dictionaries can use a single flag token that spans
// multiple code points, for example emoji plus a variation selector.
func splitUTF8Flags(raw string) []string {
	runes := []rune(raw)
	flags := make([]string, 0, len(runes))

	for i := 0; i < len(runes); {
		next := nextUTF8FlagBoundary(runes, i)
		flags = append(flags, string(runes[i:next]))
		i = next
	}

	return flags
}

func nextUTF8FlagBoundary(runes []rune, start int) int {
	i := start + 1

	if isRegionalIndicator(runes[start]) && i < len(runes) && isRegionalIndicator(runes[i]) {
		i++
	}

	for i < len(runes) {
		switch {
		case isUTF8FlagExtend(runes[i]):
			i++
		case runes[i] == zeroWidthJoiner:
			if i+1 >= len(runes) {
				return len(runes)
			}
			i += 2
		default:
			return i
		}
	}

	return i
}

func isUTF8FlagExtend(r rune) bool {
	return unicode.In(r, unicode.Mn, unicode.Mc, unicode.Me) || isEmojiModifier(r)
}

func isEmojiModifier(r rune) bool {
	return r >= 0x1f3fb && r <= 0x1f3ff
}

func isRegionalIndicator(r rune) bool {
	return r >= 0x1f1e6 && r <= 0x1f1ff
}

func (a dictConfig) tokenizeCompoundRule(rule string) ([]compoundToken, error) {
	tokens := make([]compoundToken, 0, len(rule))

	switch a.flagMode {
	case flagASCII:
		for _, r := range rule {
			if isCompoundOperator(r) {
				tokens = append(tokens, compoundToken{lit: string(r)})
				continue
			}
			tokens = append(tokens, compoundToken{flag: string(r), isFlag: true})
		}
	case flagUTF8:
		runes := []rune(rule)
		for i := 0; i < len(runes); {
			if unicode.IsSpace(runes[i]) {
				i++
				continue
			}
			if isCompoundOperator(runes[i]) {
				tokens = append(tokens, compoundToken{lit: string(runes[i])})
				i++
				continue
			}
			next := nextUTF8FlagBoundary(runes, i)
			tokens = append(tokens, compoundToken{
				flag:   string(runes[i:next]),
				isFlag: true,
			})
			i = next
		}
	case flagLong:
		runes := []rune(rule)
		for i := 0; i < len(runes); {
			if unicode.IsSpace(runes[i]) {
				i++
				continue
			}
			if isCompoundOperator(runes[i]) {
				tokens = append(tokens, compoundToken{lit: string(runes[i])})
				i++
				continue
			}
			if i+1 >= len(runes) {
				return nil, fmt.Errorf("invalid long-flag COMPOUNDRULE %q", rule)
			}
			tokens = append(tokens, compoundToken{
				flag:   string(runes[i : i+2]),
				isFlag: true,
			})
			i += 2
		}
	case flagNum:
		runes := []rune(rule)
		for i := 0; i < len(runes); {
			switch {
			case unicode.IsSpace(runes[i]) || runes[i] == ',':
				i++
			case isCompoundOperator(runes[i]):
				tokens = append(tokens, compoundToken{lit: string(runes[i])})
				i++
			case unicode.IsDigit(runes[i]):
				start := i
				for i < len(runes) && unicode.IsDigit(runes[i]) {
					i++
				}
				tokens = append(tokens, compoundToken{
					flag:   string(runes[start:i]),
					isFlag: true,
				})
			default:
				return nil, fmt.Errorf("invalid num-flag COMPOUNDRULE %q", rule)
			}
		}
	default:
		return nil, fmt.Errorf("unknown flag mode: %d", a.flagMode)
	}

	return tokens, nil
}

func (a dictConfig) registerCompoundRuleFlags(rule string) {
	tokens, err := a.tokenizeCompoundRule(rule)
	if err != nil {
		for _, r := range rule {
			if isCompoundOperator(r) {
				continue
			}
			flag := string(r)
			if _, ok := a.compoundMap[flag]; ok {
				continue
			}
			a.compoundMap[flag] = []string{}
		}
		return
	}

	for _, token := range tokens {
		if !token.isFlag {
			continue
		}
		if _, ok := a.compoundMap[token.flag]; ok {
			continue
		}
		a.compoundMap[token.flag] = []string{}
	}
}
