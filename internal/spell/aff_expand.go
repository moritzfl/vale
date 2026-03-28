package spell

// expand expands a word/affix using dictionary/affix rules.
//
// This also supports CompoundRule flags.
func (a dictConfig) expand(wordAffix string, out []derivedWord) ([]derivedWord, error) {
	out = out[:0]
	word, keyString, hasFlags, err := splitWordFlags(wordAffix)
	if err != nil {
		return nil, err
	}
	if !hasFlags {
		out = append(out, derivedWord{word: word})
		return out, nil
	}

	compoundOnly := false
	flags := flagRunes(keyString)
	for _, key := range flags {
		if isCompoundOnlyFlag(a.CompoundOnly, key) {
			compoundOnly = true
			continue
		}
		if _, ok := a.compoundMap[key]; !ok {
			// this isn't a compound flag
			continue
		}
		// is a compound flag
		a.compoundMap[key] = append(a.compoundMap[key], word)
	}

	if compoundOnly {
		return out, nil
	}

	out = append(out, derivedWord{word: word})
	prefixes := make([]flaggedAffix, 0, 5)
	suffixes := make([]flaggedAffix, 0, 5)
	for _, key := range flags {
		af, ok := a.AffixMap[key]
		if !ok {
			// TODO: How should we handle this?
			continue
		}
		if !af.CrossProduct {
			out = af.expand(word, "", key, out)
			continue
		}
		if af.Type == Prefix {
			prefixes = append(prefixes, flaggedAffix{flag: key, affix: af})
		} else {
			suffixes = append(suffixes, flaggedAffix{flag: key, affix: af})
		}
	}

	// expand all suffixes with out any prefixes
	for _, suf := range suffixes {
		out = suf.affix.expand(word, "", suf.flag, out)
	}
	for _, pre := range prefixes {
		prewords := pre.affix.expand(word, "", pre.flag, nil)
		out = append(out, prewords...)

		// now do cross product
		for _, suf := range suffixes {
			for _, w := range prewords {
				out = suf.affix.expand(w.word, w.lineage, suf.flag, out)
			}
		}
	}
	return out, nil
}
