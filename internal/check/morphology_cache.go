package check

import (
	"sync"

	"golang.org/x/sync/singleflight"

	"github.com/errata-ai/vale/v3/internal/spell"
)

var (
	morphologyCheckerCache sync.Map
	morphologyCheckerGroup singleflight.Group
)

func loadMorphologyChecker(
	cacheKey string,
	build func() (*spell.Checker, error),
) (*spell.Checker, error) {
	if cached, ok := morphologyCheckerCache.Load(cacheKey); ok {
		return cached.(*spell.Checker), nil
	}

	value, err, _ := morphologyCheckerGroup.Do(cacheKey, func() (interface{}, error) {
		if cached, ok := morphologyCheckerCache.Load(cacheKey); ok {
			return cached.(*spell.Checker), nil
		}

		checker, buildErr := build()
		if buildErr != nil {
			return nil, buildErr
		}

		cached, _ := morphologyCheckerCache.LoadOrStore(cacheKey, checker)
		return cached.(*spell.Checker), nil
	})
	if err != nil {
		return nil, err
	}

	return value.(*spell.Checker), nil
}
