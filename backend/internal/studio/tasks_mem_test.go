package studio

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// memTaskStore TaskStore 的内存实现，供 worker 单测使用（状态机语义与 pg 实现一致）。
type memTaskStore struct {
	mu     sync.Mutex
	nextID int64
	items  map[int64]*Task
}

func newMemTaskStore() *memTaskStore {
	return &memTaskStore{items: make(map[int64]*Task)}
}

func (s *memTaskStore) Create(_ context.Context, t *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	t.ID = s.nextID
	if t.PublicID == "" {
		t.PublicID = fmt.Sprintf("mem-%d", t.ID)
	}
	if t.MaxAttempts <= 0 {
		t.MaxAttempts = 3
	}
	t.Status = TaskStatusPending
	t.CreatedAt = time.Now().UTC()
	cp := *t
	s.items[t.ID] = &cp
	return nil
}

func (s *memTaskStore) GetByID(_ context.Context, userID, id int64) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.items[id]
	if !ok || t.UserID != userID {
		return nil, ErrTaskNotFound
	}
	cp := *t
	return &cp, nil
}

func (s *memTaskStore) List(_ context.Context, userID int64, status string, limit, offset int) ([]*Task, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var all []*Task
	for _, t := range s.items {
		if t.UserID != userID {
			continue
		}
		if status != "" && t.Status != status {
			continue
		}
		cp := *t
		all = append(all, &cp)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID > all[j].ID })
	total := len(all)
	if offset >= len(all) {
		return nil, total, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], total, nil
}

func (s *memTaskStore) Delete(_ context.Context, userID, id int64) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.items[id]
	if !ok || t.UserID != userID {
		return nil, ErrTaskNotFound
	}
	delete(s.items, id)
	return t, nil
}

func (s *memTaskStore) ClaimNext(_ context.Context) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var candidate *Task
	for _, t := range s.items {
		if t.Status != TaskStatusPending {
			continue
		}
		if candidate == nil || t.ID < candidate.ID {
			candidate = t
		}
	}
	if candidate == nil {
		return nil, nil
	}
	now := time.Now().UTC()
	candidate.Status = TaskStatusProcessing
	candidate.StartedAt = &now
	candidate.Progress = 50
	cp := *candidate
	return &cp, nil
}

func (s *memTaskStore) Complete(_ context.Context, id int64, output map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.items[id]
	if !ok {
		return ErrTaskNotFound
	}
	now := time.Now().UTC()
	t.Status = TaskStatusCompleted
	t.Output = output
	t.Progress = 100
	t.ErrorMessage = ""
	t.CompletedAt = &now
	return nil
}

func (s *memTaskStore) Fail(_ context.Context, id int64, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.items[id]
	if !ok {
		return ErrTaskNotFound
	}
	t.Attempts++
	t.ErrorMessage = errMsg
	if t.Attempts >= t.MaxAttempts {
		now := time.Now().UTC()
		t.Status = TaskStatusFailed
		t.Progress = 100
		t.CompletedAt = &now
	} else {
		t.Status = TaskStatusPending
		t.Progress = 0
		t.CompletedAt = nil
	}
	return nil
}

func (s *memTaskStore) ResetProcessing(_ context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for _, t := range s.items {
		if t.Status == TaskStatusProcessing {
			t.Status = TaskStatusPending
			t.StartedAt = nil
			t.Progress = 0
			n++
		}
	}
	return n, nil
}

// memUserStore UserStore 的内存实现。
type memUserStore struct {
	mu     sync.Mutex
	nextID int64
	byID   map[int64]*User
	// keys 用户按组 key：user_id → core_group_id → sk- key。
	keys map[int64]map[int64]string
}

func newMemUserStore() *memUserStore {
	return &memUserStore{byID: make(map[int64]*User), keys: make(map[int64]map[int64]string)}
}

func (s *memUserStore) UpsertKey(_ context.Context, userID, coreGroupID int64, apiKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keys[userID] == nil {
		s.keys[userID] = make(map[int64]string)
	}
	s.keys[userID][coreGroupID] = apiKey
	return nil
}

func (s *memUserStore) KeysByUser(_ context.Context, userID int64) (map[int64]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[int64]string, len(s.keys[userID]))
	for gid, key := range s.keys[userID] {
		out[gid] = key
	}
	return out, nil
}

func (s *memUserStore) Upsert(_ context.Context, airgateUserID int64, email, username, apiKey string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.byID {
		if u.AirgateUserID == airgateUserID {
			u.Email, u.Username, u.APIKey = email, username, apiKey
			u.UpdatedAt = time.Now().UTC()
			cp := *u
			return &cp, nil
		}
	}
	s.nextID++
	u := &User{
		ID: s.nextID, AirgateUserID: airgateUserID,
		Email: email, Username: username, APIKey: apiKey,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	s.byID[u.ID] = u
	cp := *u
	return &cp, nil
}

func (s *memUserStore) GetByID(_ context.Context, id int64) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.byID[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	cp := *u
	return &cp, nil
}
