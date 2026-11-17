package store

import (
	"fmt"
	"strconv"
	"testing"
	"time"
)

func TestNowUsesInjectedClock(t *testing.T) {
	clock := newFakeClock()
	dataStore := NewWithClock(clock.Now)

	clock.Advance(42 * time.Minute)

	if got := dataStore.Now(); !got.Equal(clock.Now()) {
		t.Fatalf("got %v, want %v", got, clock.Now())
	}
}

func TestSetWithDeadlineInTheFuture(t *testing.T) {
	clock := newFakeClock()
	dataStore := NewWithClock(clock.Now)
	deadline := clock.Now().Add(time.Minute)

	dataStore.SetWithDeadline("token", "abc", deadline)

	clock.Advance(time.Minute)
	if value, found := dataStore.Get("token"); !found || value != "abc" {
		t.Fatalf("at the deadline: got (%q, %v), want (\"abc\", true)", value, found)
	}

	clock.Advance(time.Nanosecond)
	if _, found := dataStore.Get("token"); found {
		t.Fatal("key should be gone one nanosecond after its deadline")
	}
}

func TestSetWithDeadlineInThePastRemovesKeyImmediately(t *testing.T) {
	clock := newFakeClock()
	dataStore := NewWithClock(clock.Now)
	dataStore.SetWithExpiry("token", "old", time.Hour)

	dataStore.SetWithDeadline("token", "new", clock.Now().Add(-time.Millisecond))

	if dataStore.Len() != 0 {
		t.Fatalf("got %d keys, want 0: a deadline in the past must not leave the key in memory", dataStore.Len())
	}
	if tracked := keysWithExpiryCount(dataStore); tracked != 0 {
		t.Fatalf("got %d tracked expiring keys, want 0", tracked)
	}
}

func TestSetWithDeadlineExactlyNowIsStillReadable(t *testing.T) {
	clock := newFakeClock()
	dataStore := NewWithClock(clock.Now)

	dataStore.SetWithDeadline("edge", "v", clock.Now())

	if value, found := dataStore.Get("edge"); !found || value != "v" {
		t.Fatalf("got (%q, %v), want (\"v\", true)", value, found)
	}
}

func TestEveryReadPathHidesExpiredKeys(t *testing.T) {
	clock := newFakeClock()
	dataStore := NewWithClock(clock.Now)
	dataStore.SetWithExpiry("expired", "41", time.Second)
	clock.Advance(2 * time.Second)

	if _, found := dataStore.Get("expired"); found {
		t.Error("Get returned an expired key")
	}
	dataStore.SetWithExpiry("expired", "41", time.Second)
	clock.Advance(2 * time.Second)

	if dataStore.Exists("expired") {
		t.Error("Exists reported an expired key")
	}
	if keys := dataStore.Keys(func(string) bool { return true }); len(keys) != 0 {
		t.Errorf("Keys returned expired keys %q", keys)
	}
	if deleted := dataStore.Delete("expired"); deleted != 0 {
		t.Errorf("Delete counted an expired key")
	}
}

func BenchmarkGetParallel(b *testing.B) {
	dataStore := New()
	for index := range 1000 {
		dataStore.Set("key:"+strconv.Itoa(index), "value")
	}

	b.ReportAllocs()
	b.RunParallel(func(parallel *testing.PB) {
		index := 0
		for parallel.Next() {
			dataStore.Get("key:" + strconv.Itoa(index%1000))
			index++
		}
	})
}

func BenchmarkSetParallel(b *testing.B) {
	dataStore := New()

	b.ReportAllocs()
	b.RunParallel(func(parallel *testing.PB) {
		index := 0
		for parallel.Next() {
			dataStore.Set("key:"+strconv.Itoa(index%1000), "value")
			index++
		}
	})
}

func BenchmarkIncrementParallel(b *testing.B) {
	dataStore := New()

	b.ReportAllocs()
	b.RunParallel(func(parallel *testing.PB) {
		for parallel.Next() {
			dataStore.Increment("counter", 1)
		}
	})
}

func BenchmarkSweeperCycleWithManyExpiredKeys(b *testing.B) {
	clock := newFakeClock()
	config := DefaultSweeperConfig()
	config.Interval = time.Hour

	for b.Loop() {
		b.StopTimer()
		dataStore := NewWithClock(clock.Now)
		for index := range 10000 {
			dataStore.SetWithExpiry(fmt.Sprintf("key:%d", index), "v", time.Second)
		}
		clock.Advance(2 * time.Second)
		sweeper := StartSweeper(dataStore, config)
		b.StartTimer()

		sweeper.RunCycle()

		b.StopTimer()
		sweeper.Stop()
		b.StartTimer()
	}
}
