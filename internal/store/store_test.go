package store

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestGetMissingKeyReturnsNotFound(t *testing.T) {
	dataStore := New()

	if value, found := dataStore.Get("missing"); found || value != "" {
		t.Fatalf("got (%q, %v), want (\"\", false)", value, found)
	}
}

func TestSetThenGetReturnsValue(t *testing.T) {
	dataStore := New()
	dataStore.Set("greeting", "hello")

	value, found := dataStore.Get("greeting")
	if !found || value != "hello" {
		t.Fatalf("got (%q, %v), want (\"hello\", true)", value, found)
	}
}

func TestSetOverwritesExistingValue(t *testing.T) {
	dataStore := New()
	dataStore.Set("counter", "1")
	dataStore.Set("counter", "2")

	if value, _ := dataStore.Get("counter"); value != "2" {
		t.Fatalf("got %q, want \"2\"", value)
	}
}

func TestEmptyStringIsAValidValue(t *testing.T) {
	dataStore := New()
	dataStore.Set("empty", "")

	value, found := dataStore.Get("empty")
	if !found || value != "" {
		t.Fatalf("got (%q, %v), want (\"\", true)", value, found)
	}
}

func TestConcurrentReadersAndWriters(t *testing.T) {
	dataStore := New()

	const workerCount = 20
	const operationsPerWorker = 500

	var workers sync.WaitGroup
	for workerIndex := range workerCount {
		workers.Go(func() {
			for operationIndex := range operationsPerWorker {
				key := fmt.Sprintf("worker-%d-key-%d", workerIndex, operationIndex%10)
				dataStore.Set(key, fmt.Sprint(operationIndex))
				dataStore.Get(key)
				dataStore.Get("shared-key")
			}
		})
	}
	workers.Wait()

	for workerIndex := range workerCount {
		key := fmt.Sprintf("worker-%d-key-9", workerIndex)
		if value, _ := dataStore.Get(key); value != fmt.Sprint(operationsPerWorker-1) {
			t.Fatalf("%s: got %q, want %q", key, value, fmt.Sprint(operationsPerWorker-1))
		}
	}
}

type fakeClock struct {
	mutex   sync.Mutex
	current time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{current: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.current
}

func (c *fakeClock) Advance(duration time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.current = c.current.Add(duration)
}

func storedKeyCount(dataStore *Store) int {
	dataStore.mutex.RLock()
	defer dataStore.mutex.RUnlock()
	return len(dataStore.data)
}

func TestKeyWithExpiryIsReadableBeforeDeadline(t *testing.T) {
	clock := newFakeClock()
	dataStore := NewWithClock(clock.Now)
	dataStore.SetWithExpiry("session", "abc", 10*time.Second)

	clock.Advance(10 * time.Second)

	if value, found := dataStore.Get("session"); !found || value != "abc" {
		t.Fatalf("at the deadline: got (%q, %v), want (\"abc\", true)", value, found)
	}
}

func TestExpiredKeyIsHiddenAndDeletedOnRead(t *testing.T) {
	clock := newFakeClock()
	dataStore := NewWithClock(clock.Now)
	dataStore.SetWithExpiry("session", "abc", 10*time.Second)

	clock.Advance(10*time.Second + time.Millisecond)

	if storedKeyCount(dataStore) != 1 {
		t.Fatal("expired key should still occupy memory before anyone reads it")
	}
	if value, found := dataStore.Get("session"); found {
		t.Fatalf("got (%q, %v), want not found", value, found)
	}
	if storedKeyCount(dataStore) != 0 {
		t.Fatal("expired key should be deleted after it is read")
	}
}

func TestPlainSetRemovesPreviousExpiry(t *testing.T) {
	clock := newFakeClock()
	dataStore := NewWithClock(clock.Now)
	dataStore.SetWithExpiry("token", "old", time.Second)
	dataStore.Set("token", "new")

	clock.Advance(time.Hour)

	if value, found := dataStore.Get("token"); !found || value != "new" {
		t.Fatalf("got (%q, %v), want (\"new\", true)", value, found)
	}
}

func TestSetWithExpiryReplacesPreviousExpiry(t *testing.T) {
	clock := newFakeClock()
	dataStore := NewWithClock(clock.Now)
	dataStore.SetWithExpiry("token", "short", time.Second)
	dataStore.SetWithExpiry("token", "long", time.Minute)

	clock.Advance(30 * time.Second)

	if value, found := dataStore.Get("token"); !found || value != "long" {
		t.Fatalf("got (%q, %v), want (\"long\", true)", value, found)
	}
}

func TestLazyDeleteDoesNotRemoveFreshlyWrittenValue(t *testing.T) {
	clock := newFakeClock()
	dataStore := NewWithClock(clock.Now)
	dataStore.SetWithExpiry("race", "expired", time.Second)
	clock.Advance(2 * time.Second)

	var workers sync.WaitGroup
	for range 20 {
		workers.Go(func() {
			dataStore.Get("race")
		})
	}
	workers.Go(func() {
		dataStore.Set("race", "fresh")
	})
	workers.Wait()

	if value, found := dataStore.Get("race"); !found || value != "fresh" {
		t.Fatalf("got (%q, %v), want (\"fresh\", true)", value, found)
	}
}
