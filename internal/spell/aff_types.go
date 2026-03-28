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
func (a affix) expand(word, lineage string, flag rune, out []derivedWord) []derivedWord {
	for i, r := range a.Rules {
		if r.matcher != nil && !r.matcher.MatchString(word) {
			continue
		}

		step := lineageStep(flag, a.Type, i)
		nextLineage := appendLineage(lineage, step)
		if a.Type == Prefix {
			out = append(out, derivedWord{word: r.AffixText + word, lineage: nextLineage})
			// TODO is does Strip apply to prefixes too?
		} else {
			stripWord := word
			if r.Strip != "" && strings.HasSuffix(word, r.Strip) {
				stripWord = word[:len(word)-len(r.Strip)]
			}
			out = append(out, derivedWord{word: stripWord + r.AffixText, lineage: nextLineage})
		}
	}
	return out
}

// rule is a Affix rule.
type rule struct {
	Strip     string
	AffixText string         // suffix or prefix text to add
	Pattern   string         // original matching pattern from AFF file
	matcher   *regexp.Regexp // matcher to see if this rule applies or not
}

// dictConfig is a partial representation of a Hunspell AFF (Affix) file.
type dictConfig struct {
	IconvReplacements []string
	Replacements      [][2]string
	CompoundRule      []string
	Flag              string
	TryChars          string
	WordChars         string
	CompoundOnly      string
	AffixMap          map[rune]affix
	CamelCase         int
	CompoundMin       int64
	compoundMap       map[rune][]string
	NoSuggestFlag     string
}

type derivedWord struct {
	word    string
	lineage string
}

type flaggedAffix struct {
	flag  rune
	affix affix
}

func lineageStep(flag rune, typ affixType, ruleIndex int) string {
	prefix := "S"
	if typ == Prefix {
		prefix = "P"
	}

	return prefix + ":" + string(flag) + ":" + strconv.Itoa(ruleIndex)
}

func appendLineage(existing, step string) string {
	if existing == "" {
		return step
	}

	return existing + "|" + step
}
