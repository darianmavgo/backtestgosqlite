package strategy

import (
	"sort"
	"strings"
	"sync"
)

var (
	registryLock sync.RWMutex
	registry     = make(map[string]Strategy)
)

// Register adds a strategy to the central registry.
func Register(s Strategy) {
	registryLock.Lock()
	defer registryLock.Unlock()
	registry[strings.ToLower(s.ID())] = s
}

// RegisterAlias registers an alternate lookup key/alias for a strategy.
func RegisterAlias(alias string, s Strategy) {
	registryLock.Lock()
	defer registryLock.Unlock()
	registry[strings.ToLower(alias)] = s
}

func normalizeKey(k string) string {
	k = strings.ToLower(k)
	k = strings.ReplaceAll(k, "-", "")
	k = strings.ReplaceAll(k, "_", "")
	k = strings.ReplaceAll(k, " ", "")
	return k
}

// Get retrieves a strategy by its ID or alias (case-insensitive and punctuation-agnostic).
func Get(id string) (Strategy, bool) {
	registryLock.RLock()
	defer registryLock.RUnlock()

	// 1. Direct match (ID or registered alias)
	if s, found := registry[strings.ToLower(id)]; found {
		return s, true
	}

	// 2. Normalized match (ignores case, dashes, underscores, and spaces)
	target := normalizeKey(id)
	for key, s := range registry {
		if normalizeKey(key) == target || normalizeKey(s.ID()) == target {
			return s, true
		}
	}

	return nil, false
}

// List returns all registered strategies sorted alphabetically by ID.
func List() []Strategy {
	registryLock.RLock()
	defer registryLock.RUnlock()

	seen := make(map[string]bool)
	var list []Strategy
	for _, s := range registry {
		if !seen[s.ID()] {
			seen[s.ID()] = true
			list = append(list, s)
		}
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].ID() < list[j].ID()
	})
	return list
}
