package spell

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

func isCrossProduct(val string) (bool, error) {
	switch val {
	case "Y":
		return true, nil
	case "N":
		return false, nil
	}
	return false, fmt.Errorf("CrossProduct is not Y or N: got %q", val)
}

// newDictConfig reads an Hunspell AFF file.
func newDictConfig(file io.Reader) (*dictConfig, error) { //nolint:funlen
	aff := dictConfig{
		Flag:         "ASCII",
		flagMode:     flagASCII,
		AffixMap:     make(map[string]affix),
		CompoundOnly: make(map[string]struct{}),
		compoundMap:  make(map[string][]string),
		CompoundMin:  3, // default in Hunspell
	}

	expectedAF := -1
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		switch parts[0] {
		case "TRY":
			if len(parts) < 2 {
				return nil, fmt.Errorf("TRY stanza had %d fields, expected 2", len(parts))
			}
			aff.TryChars = parts[1]
		case "ICONV":
			// if only 2 fields, then its the first stanza that just provides a count
			//  we don't care, as we dynamically allocate
			if len(parts) == 2 {
				continue
			} else if len(parts) < 3 {
				return nil, fmt.Errorf("ICONV stanza had %d fields, expected 2", len(parts))
			}
			aff.IconvReplacements = append(aff.IconvReplacements, parts[1], parts[2])
		case "REP":
			if len(parts) == 2 {
				continue
			} else if len(parts) < 3 {
				return nil, fmt.Errorf("REP stanza had %d fields, expected 2", len(parts))
			}
			aff.Replacements = append(aff.Replacements, [2]string{parts[1], parts[2]})
		case "COMPOUNDMIN":
			if len(parts) < 2 {
				return nil, fmt.Errorf("COMPOUNDMIN stanza had %d fields, expected 2", len(parts))
			}
			val, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("COMPOUNDMIN stanza had %q expected number", parts[1])
			}
			aff.CompoundMin = val
		case "ONLYINCOMPOUND":
			if len(parts) < 2 {
				return nil, fmt.Errorf("ONLYINCOMPOUND stanza had %d fields, expected 2", len(parts))
			}
			flags, err := aff.parseFlags(parts[1])
			if err != nil {
				return nil, err
			}
			for _, flag := range flags {
				aff.CompoundOnly[flag] = struct{}{}
			}
		case "COMPOUNDRULE":
			if len(parts) < 2 {
				return nil, fmt.Errorf("COMPOUNDRULE stanza had %d fields, expected 2", len(parts))
			}
			val, err := strconv.ParseInt(parts[1], 10, 64)
			if err == nil {
				aff.CompoundRule = make([]string, 0, val)
			} else {
				aff.CompoundRule = append(aff.CompoundRule, parts[1])
				aff.registerCompoundRuleFlags(parts[1])
			}
		case "NOSUGGEST":
			if len(parts) < 2 {
				return nil, fmt.Errorf("NOSUGGEST stanza had %d fields, expected 2", len(parts))
			}
			flag, err := aff.parseSingleFlag(parts[1])
			if err != nil {
				return nil, err
			}
			aff.NoSuggestFlag = flag
		case "WORDCHARS":
			if len(parts) < 2 {
				return nil, fmt.Errorf("WORDCHAR stanza had %d fields, expected 2", len(parts))
			}
			aff.WordChars = parts[1]
		case "FLAG":
			if len(parts) < 2 {
				return nil, fmt.Errorf("FLAG stanza had %d, expected 1", len(parts))
			}
			mode, label, err := parseFlagMode(parts[1])
			if err != nil {
				return nil, err
			}
			aff.flagMode = mode
			aff.Flag = label
		case "AF":
			if len(parts) < 2 {
				return nil, fmt.Errorf("AF stanza had %d fields, expected at least 2", len(parts))
			}

			if expectedAF == -1 && len(parts) == 2 {
				if count, err := strconv.Atoi(parts[1]); err == nil {
					expectedAF = count
					aff.flagAliases = make([][]string, 0, count)
					continue
				}
			}

			flags, err := aff.parseFlags(parts[1])
			if err != nil {
				return nil, err
			}
			aff.flagAliases = append(aff.flagAliases, flags)
		case "PFX", "SFX":
			atype := Prefix
			if parts[0] == "SFX" {
				atype = Suffix
			}

			sections := len(parts)
			if sections >= 5 {
				flag, err := aff.parseSingleFlag(parts[1])
				if err != nil {
					return nil, err
				}
				a, ok := aff.AffixMap[flag]
				if !ok {
					return nil, fmt.Errorf("got rules for flag %q but no definition", flag)
				}

				strip := ""
				if parts[2] != "0" {
					strip = parts[2]
				}

				var matcher *regexp.Regexp
				pat := parts[4]
				if pat != "." {
					if a.Type == Prefix {
						pat = "^" + pat
					} else {
						pat += "$"
					}
					matcher, err = regexp.Compile(pat)
					if err != nil {
						return nil, fmt.Errorf("unable to compile %s", pat)
					}
				}

				affixText, continuation, err := aff.splitAffixAndContinuation(parts[3])
				if err != nil {
					return nil, err
				}

				a.Rules = append(a.Rules, rule{
					Strip:             strip,
					AffixText:         affixText,
					Pattern:           parts[4],
					ContinuationFlags: continuation,
					matcher:           matcher,
				})
				aff.AffixMap[flag] = a
			} else if sections >= 4 {
				cross, err := isCrossProduct(parts[2])
				if err != nil {
					return nil, err
				}
				// this is a new Affix!
				a := affix{
					Type:         atype,
					CrossProduct: cross,
				}
				flag, err := aff.parseSingleFlag(parts[1])
				if err != nil {
					return nil, err
				}
				aff.AffixMap[flag] = a
			}
		default:
			// Do nothing.
			//
			// Hunspell ignores lines that don't start with a known directive.
		}
	}

	if expectedAF >= 0 && len(aff.flagAliases) != expectedAF {
		return nil, fmt.Errorf("AF stanza expected %d aliases, got %d", expectedAF, len(aff.flagAliases))
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return &aff, nil
}
