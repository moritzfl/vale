package spell

import (
	"regexp"
	"strconv"
	"strings"
)

// affixType is either an affix prefix or suffix.
type affixType int

// specific Affix types
const (
	Prefix affixType = iota
	Suffix
)

// affix is a rule for affix (adding prefixes or suffixes).
type affix struct {
	Rules        []rule    // -
	Type         affixType // either PFX or SFX
	CrossProduct bool      // -
}

// expand provides all variations of a given word based on this affix rule.
func (a affix) expand(word, lineage, flag string, out []derivedWord) []derivedWord {
	for i, r := range a.Rules {
		if r.matcher != nil && !r.matcher.MatchString(word) {
			continue
		}

		step := lineageStep(flag, a.Type, i)
		nextLineage := appendLineage(lineage, step)
		if a.Type == Prefix {
			stripWord := word
			if r.Strip != "" {
				if !strings.HasPrefix(word, r.Strip) {
					continue
				}
				stripWord = word[len(r.Strip):]
			}
			out = append(out, derivedWord{
				word:              r.AffixText + stripWord,
				lineage:           nextLineage,
				continuationFlags: cloneFlags(r.ContinuationFlags),
			})
		} else {
			stripWord := word
			if r.Strip != "" {
				if !strings.HasSuffix(word, r.Strip) {
					continue
				}
				stripWord = word[:len(word)-len(r.Strip)]
			}
			out = append(out, derivedWord{
				word:              stripWord + r.AffixText,
				lineage:           nextLineage,
				continuationFlags: cloneFlags(r.ContinuationFlags),
			})
		}
	}
	return out
}

// rule is a Affix rule.
type rule struct {
	Strip             string
	AffixText         string // suffix or prefix text to add
	Pattern           string // original matching pattern from AFF file
	ContinuationFlags []string
	matcher           *regexp.Regexp // matcher to see if this rule applies or not
}

// dictConfig is a partial representation of a Hunspell AFF (Affix) file.
type dictConfig struct {
	IconvReplacements []string
	Replacements      [][2]string
	CompoundRule      []string
	Flag              string
	TryChars          string
	WordChars         string
	CompoundOnly      map[string]struct{}
	AffixMap          map[string]affix
	CamelCase         int
	CompoundMin       int64
	compoundMap       map[string][]string
	flagAliases       [][]string
	flagMode          flagMode
	NoSuggestFlag     string
}

type derivedWord struct {
	word              string
	lineage           string
	continuationFlags []string
}

type flaggedAffix struct {
	flag  string
	affix affix
}

type flagMode int

const (
	flagASCII flagMode = iota
	flagUTF8
	flagLong
	flagNum
)

type compoundToken struct {
	flag   string
	lit    string
	isFlag bool
}

func lineageStep(flag string, typ affixType, ruleIndex int) string {
	prefix := "S"
	if typ == Prefix {
		prefix = "P"
	}

	return prefix + ":" + flag + ":" + strconv.Itoa(ruleIndex)
}

func appendLineage(existing, step string) string {
	if existing == "" {
		return step
	}

	return existing + "|" + step
}

func cloneFlags(flags []string) []string {
	if len(flags) == 0 {
		return nil
	}

	out := make([]string, len(flags))
	copy(out, flags)
	return out
}

func mergeFlags(flags ...[]string) []string {
	seen := make(map[string]struct{})
	merged := make([]string, 0, 4)

	for _, set := range flags {
		for _, flag := range set {
			if _, ok := seen[flag]; ok {
				continue
			}
			seen[flag] = struct{}{}
			merged = append(merged, flag)
		}
	}

	return merged
}

func joinFlags(flags []string) string {
	if len(flags) == 0 {
		return ""
	}
	return strings.Join(flags, "\x1f")
}
