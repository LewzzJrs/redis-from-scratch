package store

import (
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDeleteCountsOnlyLiveKeys(t *testing.T) {
	clock := newFakeClock()
	dataStore := NewWithClock(clock.Now)
	dataStore.Set("a", "1")
	dataStore.Set("b", "2")
	dataStore.SetWithExpiry("expired", "3", time.Second)
	clock.Advance(2 * time.Second)

	deleted := dataStore.Delete("a", "b", "missing", "expired", "a")

	if deleted != 2 {
		t.Fatalf("got %d deleted, want 2", deleted)
	}
	if remaining := dataStore.Len(); remaining != 0 {
		t.Fatalf("got %d keys left, want 0 (expired key should be cleaned up too)", remaining)
	}
	if tracked := keysWithExpiryCount(dataStore); tracked != 0 {
		t.Fatalf("got %d tracked expiring keys, want 0", tracked)
	}
}

func TestExists(t *testing.T) {
	clock := newFakeClock()
	dataStore := NewWithClock(clock.Now)
	dataStore.Set("live", "1")
	dataStore.SetWithExpiry("expiring", "2", time.Second)
	clock.Advance(2 * time.Second)

	if !dataStore.Exists("live") {
		t.Error("live key should exist")
	}
	if dataStore.Exists("expiring") {
		t.Error("expired key should not exist")
	}
	if dataStore.Exists("missing") {
		t.Error("missing key should not exist")
	}
}

func TestKeysSkipsExpiredAndFilters(t *testing.T) {
	clock := newFakeClock()
	dataStore := NewWithClock(clock.Now)
	dataStore.Set("user:1", "a")
	dataStore.Set("user:2", "b")
	dataStore.Set("session:1", "c")
	dataStore.SetWithExpiry("user:3", "d", time.Second)
	clock.Advance(2 * time.Second)

	userKeys := dataStore.Keys(func(key string) bool {
		return strings.HasPrefix(key, "user:")
	})
	slices.Sort(userKeys)

	if want := []string{"user:1", "user:2"}; !slices.Equal(userKeys, want) {
		t.Fatalf("got %q, want %q", userKeys, want)
	}
}

func TestKeysOnEmptyStoreReturnsEmptySlice(t *testing.T) {
	keys := New().Keys(func(string) bool { return true })
	if keys == nil || len(keys) != 0 {
		t.Fatalf("got %#v, want empty non-nil slice", keys)
	}
}

func TestIncrementMissingKeyStartsFromZero(t *testing.T) {
	dataStore := New()

	for expected := int64(1); expected <= 3; expected++ {
		got, err := dataStore.Increment("counter", 1)
		if err != nil || got != expected {
			t.Fatalf("got (%d, %v), want (%d, nil)", got, err, expected)
		}
	}
	if value, _ := dataStore.Get("counter"); value != "3" {
		t.Fatalf("stored value %q, want \"3\"", value)
	}
}

func TestIncrementExistingNumbers(t *testing.T) {
	testCases := []struct {
		initial  string
		delta    int64
		expected int64
	}{
		{"10", 1, 11},
		{"-5", 1, -4},
		{"-1", 1, 0},
		{"0", -1, -1},
	}

	for _, testCase := range testCases {
		dataStore := New()
		dataStore.Set("n", testCase.initial)

		got, err := dataStore.Increment("n", testCase.delta)
		if err != nil || got != testCase.expected {
			t.Errorf("%s %+d: got (%d, %v), want (%d, nil)", testCase.initial, testCase.delta, got, err, testCase.expected)
		}
	}
}

func TestIncrementRejectsNonCanonicalIntegers(t *testing.T) {
	for _, value := range []string{"abc", "", "1.5", " 1", "1 ", "+1", "01", "-0", "99999999999999999999"} {
		dataStore := New()
		dataStore.Set("n", value)

		if _, err := dataStore.Increment("n", 1); !errors.Is(err, ErrNotInteger) {
			t.Errorf("value %q: got %v, want ErrNotInteger", value, err)
		}
		if stored, _ := dataStore.Get("n"); stored != value {
			t.Errorf("value %q was modified to %q after a failed increment", value, stored)
		}
	}
}

func TestIncrementDetectsOverflow(t *testing.T) {
	dataStore := New()
	dataStore.Set("max", strconv.FormatInt(math.MaxInt64, 10))
	dataStore.Set("min", strconv.FormatInt(math.MinInt64, 10))

	if _, err := dataStore.Increment("max", 1); !errors.Is(err, ErrIncrementOverflow) {
		t.Errorf("max + 1: got %v, want ErrIncrementOverflow", err)
	}
	if _, err := dataStore.Increment("min", -1); !errors.Is(err, ErrIncrementOverflow) {
		t.Errorf("min - 1: got %v, want ErrIncrementOverflow", err)
	}
}

func TestIncrementKeepsExistingExpiry(t *testing.T) {
	clock := newFakeClock()
	dataStore := NewWithClock(clock.Now)
	dataStore.SetWithExpiry("rate:limit", "5", 10*time.Second)

	clock.Advance(5 * time.Second)
	if got, _ := dataStore.Increment("rate:limit", 1); got != 6 {
		t.Fatalf("got %d, want 6", got)
	}

	clock.Advance(6 * time.Second)
	if dataStore.Exists("rate:limit") {
		t.Fatal("increment should not have removed the original expiry")
	}
}

func TestIncrementExpiredKeyStartsOverWithoutExpiry(t *testing.T) {
	clock := newFakeClock()
	dataStore := NewWithClock(clock.Now)
	dataStore.SetWithExpiry("visits", "41", time.Second)
	clock.Advance(2 * time.Second)

	if got, _ := dataStore.Increment("visits", 1); got != 1 {
		t.Fatalf("got %d, want 1", got)
	}
	clock.Advance(time.Hour)
	if !dataStore.Exists("visits") {
		t.Fatal("restarted counter should not inherit the old expiry")
	}
	if tracked := keysWithExpiryCount(dataStore); tracked != 0 {
		t.Fatalf("got %d tracked expiring keys, want 0", tracked)
	}
}

func TestConcurrentIncrementsAreNotLost(t *testing.T) {
	dataStore := New()

	const workerCount = 50
	const incrementsPerWorker = 100

	var workers sync.WaitGroup
	for range workerCount {
		workers.Go(func() {
			for range incrementsPerWorker {
				dataStore.Increment("hits", 1)
			}
		})
	}
	workers.Wait()

	if value, _ := dataStore.Get("hits"); value != strconv.Itoa(workerCount*incrementsPerWorker) {
		t.Fatalf("got %q, want %d", value, workerCount*incrementsPerWorker)
	}
}
