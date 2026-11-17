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
	mutex          sync.RWMutex
	data           map[string]entry
	keysWithExpiry map[string]struct{}
	now            func() time.Time
}

func New() *Store {
	return NewWithClock(time.Now)
}

func NewWithClock(now func() time.Time) *Store {
	return &Store{
		data:           make(map[string]entry),
		keysWithExpiry: make(map[string]struct{}),
		now:            now,
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
	delete(s.keysWithExpiry, key)
}

func (s *Store) SetWithExpiry(key string, value string, timeToLive time.Duration) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.data[key] = entry{value: value, expiresAt: s.now().Add(timeToLive)}
	s.keysWithExpiry[key] = struct{}{}
}

func (s *Store) Len() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return len(s.data)
}

func (s *Store) DeleteExpiredSample(sampleSize int) (sampledCount int, deletedCount int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	now := s.now()
	for key := range s.keysWithExpiry {
		if sampledCount == sampleSize {
			break
		}
		sampledCount++

		if s.data[key].isExpiredAt(now) {
			s.deleteKey(key)
			deletedCount++
		}
	}
	return sampledCount, deletedCount
}

func (s *Store) deleteIfExpired(key string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	storedEntry, found := s.data[key]
	if found && storedEntry.isExpiredAt(s.now()) {
		s.deleteKey(key)
	}
}

func (s *Store) deleteKey(key string) {
	delete(s.data, key)
	delete(s.keysWithExpiry, key)
}
