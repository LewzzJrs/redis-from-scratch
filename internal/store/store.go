package store

import (
	"errors"
	"math"
	"strconv"
	"sync"
	"time"
)

var (
	ErrNotInteger        = errors.New("value is not an integer")
	ErrIncrementOverflow = errors.New("increment would overflow")
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
	s.SetWithDeadline(key, value, s.now().Add(timeToLive))
}

func (s *Store) SetWithDeadline(key string, value string, deadline time.Time) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.now().After(deadline) {
		s.deleteKey(key)
		return
	}
	s.data[key] = entry{value: value, expiresAt: deadline}
	s.keysWithExpiry[key] = struct{}{}
}

func (s *Store) Now() time.Time {
	return s.now()
}

func (s *Store) Delete(keys ...string) int {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	now := s.now()
	deletedCount := 0
	for _, key := range keys {
		storedEntry, found := s.data[key]
		if !found {
			continue
		}
		if !storedEntry.isExpiredAt(now) {
			deletedCount++
		}
		s.deleteKey(key)
	}
	return deletedCount
}

func (s *Store) Exists(key string) bool {
	_, found := s.Get(key)
	return found
}

func (s *Store) Keys(matches func(key string) bool) []string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	now := s.now()
	matchingKeys := []string{}
	for key, storedEntry := range s.data {
		if storedEntry.isExpiredAt(now) {
			continue
		}
		if matches(key) {
			matchingKeys = append(matchingKeys, key)
		}
	}
	return matchingKeys
}

func (s *Store) Increment(key string, delta int64) (newNumber int64, expiresAt time.Time, err error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	storedEntry, found := s.data[key]
	if found && storedEntry.isExpiredAt(s.now()) {
		s.deleteKey(key)
		storedEntry, found = entry{}, false
	}

	currentNumber := int64(0)
	if found {
		parsedNumber, isInteger := parseStrictInteger(storedEntry.value)
		if !isInteger {
			return 0, time.Time{}, ErrNotInteger
		}
		currentNumber = parsedNumber
	}

	if wouldOverflow(currentNumber, delta) {
		return 0, time.Time{}, ErrIncrementOverflow
	}

	newNumber = currentNumber + delta
	storedEntry.value = strconv.FormatInt(newNumber, 10)
	s.data[key] = storedEntry
	return newNumber, storedEntry.expiresAt, nil
}

func parseStrictInteger(text string) (int64, bool) {
	number, err := strconv.ParseInt(text, 10, 64)
	if err != nil || strconv.FormatInt(number, 10) != text {
		return 0, false
	}
	return number, true
}

func wouldOverflow(current int64, delta int64) bool {
	if delta > 0 {
		return current > math.MaxInt64-delta
	}
	return current < math.MinInt64-delta
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
