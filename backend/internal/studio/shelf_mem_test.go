package studio

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// memShelfStore ShelfStore 的内存实现，语义与 pg 实现一致。
type memShelfStore struct {
	mu       sync.Mutex
	groups   map[int64]*ShelfGroup
	models   map[int64]*ShelfModel // by model id
	nextID   int64
	modelKey map[string]int64 // "gid/name" → model id
}

func newMemShelfStore() *memShelfStore {
	return &memShelfStore{
		groups:   make(map[int64]*ShelfGroup),
		models:   make(map[int64]*ShelfModel),
		modelKey: make(map[string]int64),
	}
}

func shelfKey(gid int64, name string) string {
	return fmt.Sprintf("%d/%s", gid, name)
}

func (s *memShelfStore) UpsertGroups(_ context.Context, groups []ShelfGroup) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, g := range groups {
		existing, ok := s.groups[g.CoreGroupID]
		if ok {
			existing.Name, existing.RateMultiplier, existing.Note = g.Name, g.RateMultiplier, g.Note
			existing.SyncedAt = time.Now()
			continue
		}
		cp := g
		cp.SyncedAt = time.Now()
		s.groups[g.CoreGroupID] = &cp
	}
	return nil
}

func (s *memShelfStore) ListGroups(_ context.Context, onlyEnabled bool) ([]ShelfGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ShelfGroup
	for _, g := range s.groups {
		if onlyEnabled && !g.Enabled {
			continue
		}
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CoreGroupID < out[j].CoreGroupID })
	return out, nil
}

func (s *memShelfStore) SetGroupEnabled(_ context.Context, coreGroupID int64, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.groups[coreGroupID]
	if !ok {
		return ErrShelfNotFound
	}
	g.Enabled = enabled
	return nil
}

func (s *memShelfStore) SyncModels(_ context.Context, coreGroupID int64, models []ShelfModel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.models {
		if m.CoreGroupID == coreGroupID {
			m.MissingAtCore = true
		}
	}
	for _, m := range models {
		key := shelfKey(coreGroupID, m.ModelName)
		if id, ok := s.modelKey[key]; ok {
			existing := s.models[id]
			existing.Protocols = m.Protocols
			existing.MissingAtCore = false
			existing.SyncedAt = time.Now()
			continue
		}
		s.nextID++
		s.models[s.nextID] = &ShelfModel{
			ID: s.nextID, CoreGroupID: coreGroupID, ModelName: m.ModelName,
			Protocols: m.Protocols, SyncedAt: time.Now(),
		}
		s.modelKey[key] = s.nextID
	}
	return nil
}

func (s *memShelfStore) ListModels(_ context.Context, coreGroupID int64, onlyEnabled bool) ([]ShelfModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ShelfModel
	for _, m := range s.models {
		if m.CoreGroupID != coreGroupID {
			continue
		}
		if onlyEnabled && !m.Enabled {
			continue
		}
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].ModelName < out[j].ModelName
	})
	return out, nil
}

func (s *memShelfStore) GetModel(_ context.Context, coreGroupID int64, modelName string) (*ShelfModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.modelKey[shelfKey(coreGroupID, modelName)]; ok {
		cp := *s.models[id]
		return &cp, nil
	}
	return nil, ErrShelfNotFound
}

func (s *memShelfStore) UpdateModel(_ context.Context, id int64, patch ShelfModelPatch) (*ShelfModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.models[id]
	if !ok {
		return nil, ErrShelfNotFound
	}
	if patch.DisplayName != nil {
		m.DisplayName = *patch.DisplayName
	}
	if patch.Enabled != nil {
		m.Enabled = *patch.Enabled
	}
	if patch.SortOrder != nil {
		m.SortOrder = *patch.SortOrder
	}
	cp := *m
	return &cp, nil
}
