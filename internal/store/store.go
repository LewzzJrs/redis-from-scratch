package store

import (
	"sync"
	"time"
)

type entry struct {
	value     string
	expiresAt time.Time
}

func (e entry) hasExpiry() bool {
	return !e.expiresAt.IsZero()
}

func (e entry) isExpiredAt(moment time.Time) bool {
	return e.hasExpiry() && moment.After(e.expiresAt)
}

type Store struct {
	mutex sync.RWMutex
	data  map[string]entry
	now   func() time.Time
}

func New() *Store {
	return NewWithClock(time.Now)
}

func NewWithClock(now func() time.Time) *Store {
	return &Store{
		data: make(map[string]entry),
		now:  now,
	}
}

func (s *Store) Get(key string) (string, bool) {
	s.mutex.RLock()
	storedEntry, found := s.data[key]
	s.mutex.RUnlock()

	if !found {
		return "", false
	}
	if storedEntry.isExpiredAt(s.now()) {
		s.deleteIfExpired(key)
		return "", false
	}
	return storedEntry.value, true
}

func (s *Store) Set(key string, value string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.data[key] = entry{value: value}
}

func (s *Store) SetWithExpiry(key string, value string, timeToLive time.Duration) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.data[key] = entry{value: value, expiresAt: s.now().Add(timeToLive)}
}

func (s *Store) deleteIfExpired(key string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	storedEntry, found := s.data[key]
	if found && storedEntry.isExpiredAt(s.now()) {
		delete(s.data, key)
	}
}
