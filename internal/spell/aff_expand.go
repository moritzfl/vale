package spell

func (a dictConfig) expandFlags(
	state derivedWord,
	withLineage bool,
	flags []string,
	out []derivedWord,
) []derivedWord {
	prefixes := make([]flaggedAffix, 0, 5)
	suffixes := make([]flaggedAffix, 0, 5)
	for _, key := range flags {
		af, ok := a.AffixMap[key]
		if !ok {
			// Hunspell ignores unknown flags.
			continue
		}
		if !af.CrossProduct {
			out = af.expand(state, key, withLineage, out)
			continue
		}
		if af.Type == Prefix {
			prefixes = append(prefixes, flaggedAffix{flag: key, affix: af})
		} else {
			suffixes = append(suffixes, flaggedAffix{flag: key, affix: af})
		}
	}

	for _, suf := range suffixes {
		out = suf.affix.expand(state, suf.flag, withLineage, out)
	}
	for _, pre := range prefixes {
		prewords := pre.affix.expand(state, pre.flag, withLineage, nil)
		out = append(out, prewords...)

		for _, suf := range suffixes {
			for _, w := range prewords {
				derived := suf.affix.expand(w, suf.flag, withLineage, nil)
				for i := range derived {
					derived[i].continuationFlags = mergeFlags(
						w.continuationFlags,
						derived[i].continuationFlags,
					)
				}
				out = append(out, derived...)
			}
		}
	}

	return out
}

// expand expands a word/affix using dictionary/affix rules.
//
// This also supports CompoundRule flags.
func (a dictConfig) expand(wordAffix string, out []derivedWord) ([]derivedWord, error) {
	return a.expandWithLineageMode(wordAffix, true, out)
}

func (a dictConfig) expandWithoutLineage(wordAffix string, out []derivedWord) ([]derivedWord, error) {
	return a.expandWithLineageMode(wordAffix, false, out)
}

func (a dictConfig) expandWithLineageMode(
	wordAffix string,
	withLineage bool,
	out []derivedWord,
) ([]derivedWord, error) {
	out = out[:0]
	word, keyString, hasFlags, err := a.splitWordFlags(wordAffix)
	if err != nil {
		return nil, err
	}
	if !hasFlags {
		out = append(out, derivedWord{word: word})
		return out, nil
	}

	keys, err := a.resolveDictionaryFlags(keyString)
	if err != nil {
		return nil, err
	}

	compoundOnly := false
	for _, key := range keys {
		if _, ok := a.CompoundOnly[key]; ok {
			compoundOnly = true
			continue
		}
		if _, ok := a.compoundMap[key]; !ok {
			continue
		}
		a.compoundMap[key] = append(a.compoundMap[key], word)
	}

	if compoundOnly {
		return out, nil
	}

	out = append(out, derivedWord{word: word})
	stateQueue := []derivedWord{{
		word:              word,
		lineage:           "",
		lineageKey:        "",
		continuationFlags: keys,
	}}
	seenStates := map[string]struct{}{
		word + "\x00" + joinFlags(keys): {},
	}

	for len(stateQueue) > 0 {
		current := stateQueue[0]
		stateQueue = stateQueue[1:]

		expanded := a.expandFlags(
			current,
			withLineage,
			current.continuationFlags,
			nil,
		)
		for _, item := range expanded {
			next := derivedWord{word: item.word}
			if withLineage {
				next.lineage = item.lineage
				next.lineageKey = item.lineageKey
			}
			out = append(out, next)

			if len(item.continuationFlags) == 0 {
				continue
			}

			key := item.word + "\x00" + joinFlags(item.continuationFlags)
			if _, ok := seenStates[key]; ok {
				continue
			}
			seenStates[key] = struct{}{}
			stateQueue = append(stateQueue, item)
		}
	}

	return out, nil
}
