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

// Get retrieves a strategy by its ID (case-insensitive).
func Get(id string) (Strategy, bool) {
	registryLock.RLock()
	defer registryLock.RUnlock()
	s, found := registry[strings.ToLower(id)]
	return s, found
}

// List returns all registered strategies sorted alphabetically by ID.
func List() []Strategy {
	registryLock.RLock()
	defer registryLock.RUnlock()

	var list []Strategy
	for _, s := range registry {
		list = append(list, s)
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].ID() < list[j].ID()
	})
	return list
}
