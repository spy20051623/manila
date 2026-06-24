package store

import (
	"fmt"
	"sync"

	"manila/internal/model"
)

type GameRef struct {
	Mu   sync.Mutex
	Game *model.Game
}

type MemoryStore struct {
	mu    sync.RWMutex
	games map[string]*GameRef
	next  int
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{games: map[string]*GameRef{}, next: 1}
}

func (s *MemoryStore) NextID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := fmt.Sprintf("game-%d", s.next)
	s.next++
	return id
}

func (s *MemoryStore) Put(g *model.Game) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.games[g.ID] = &GameRef{Game: g}
}

func (s *MemoryStore) Get(id string) (*GameRef, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ref, ok := s.games[id]
	return ref, ok
}

func (s *MemoryStore) List() []*GameRef {
	s.mu.RLock()
	defer s.mu.RUnlock()
	refs := make([]*GameRef, 0, len(s.games))
	for _, ref := range s.games {
		refs = append(refs, ref)
	}
	return refs
}
