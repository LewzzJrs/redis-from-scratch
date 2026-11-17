package store

import "sync"

type Store struct {
	mutex sync.RWMutex
	data  map[string]string
}

func New() *Store {
	return &Store{data: make(map[string]string)}
}

func (s *Store) Get(key string) (string, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	value, found := s.data[key]
	return value, found
}

func (s *Store) Set(key string, value string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.data[key] = value
}
