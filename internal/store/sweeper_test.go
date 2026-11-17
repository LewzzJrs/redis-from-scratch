package store

import (
	"fmt"
	"testing"
	"time"
)

func startTestSweeper(t *testing.T, dataStore *Store, config SweeperConfig) *Sweeper {
	t.Helper()
	sweeper := StartSweeper(dataStore, config)
	t.Cleanup(sweeper.Stop)
	return sweeper
}

func manualSweeperConfig() SweeperConfig {
	config := DefaultSweeperConfig()
	config.Interval = time.Hour
	return config
}

func keysWithExpiryCount(dataStore *Store) int {
	dataStore.mutex.RLock()
	defer dataStore.mutex.RUnlock()
	return len(dataStore.keysWithExpiry)
}

func fillStore(dataStore *Store, prefix string, count int, timeToLive time.Duration) {
	for index := range count {
		key := fmt.Sprintf("%s:%d", prefix, index)
		if timeToLive > 0 {
			dataStore.SetWithExpiry(key, "value", timeToLive)
		} else {
			dataStore.Set(key, "value")
		}
	}
}

func TestOnlyKeysWithExpiryAreTracked(t *testing.T) {
	dataStore := NewWithClock(newFakeClock().Now)
	fillStore(dataStore, "permanent", 5, 0)
	fillStore(dataStore, "temporary", 3, time.Minute)

	if tracked := keysWithExpiryCount(dataStore); tracked != 3 {
		t.Fatalf("got %d tracked keys, want 3", tracked)
	}

	dataStore.Set("temporary:0", "now permanent")
	if tracked := keysWithExpiryCount(dataStore); tracked != 2 {
		t.Fatalf("after plain SET: got %d tracked keys, want 2", tracked)
	}
}

func TestLazyDeleteAlsoStopsTrackingKey(t *testing.T) {
	clock := newFakeClock()
	dataStore := NewWithClock(clock.Now)
	dataStore.SetWithExpiry("session", "abc", time.Second)
	clock.Advance(2 * time.Second)

	dataStore.Get("session")

	if tracked := keysWithExpiryCount(dataStore); tracked != 0 {
		t.Fatalf("got %d tracked keys, want 0", tracked)
	}
}

func TestSweeperRemovesExpiredKeysNobodyReads(t *testing.T) {
	clock := newFakeClock()
	dataStore := NewWithClock(clock.Now)
	fillStore(dataStore, "permanent", 10, 0)
	fillStore(dataStore, "long", 10, time.Hour)
	fillStore(dataStore, "short", 100, time.Second)
	clock.Advance(2 * time.Second)

	sweeper := startTestSweeper(t, dataStore, manualSweeperConfig())
	for cycle := 0; cycle < 50 && dataStore.Len() > 20; cycle++ {
		sweeper.RunCycle()
	}

	if remaining := dataStore.Len(); remaining != 20 {
		t.Fatalf("got %d keys left, want 20 (10 permanent + 10 long)", remaining)
	}
	for index := range 10 {
		if _, found := dataStore.Get(fmt.Sprintf("long:%d", index)); !found {
			t.Fatalf("long:%d was removed before its deadline", index)
		}
		if _, found := dataStore.Get(fmt.Sprintf("permanent:%d", index)); !found {
			t.Fatalf("permanent:%d was removed", index)
		}
	}
}

func TestSweeperRepeatsWhileManySampledKeysAreExpired(t *testing.T) {
	clock := newFakeClock()
	dataStore := NewWithClock(clock.Now)
	fillStore(dataStore, "short", 200, time.Second)
	clock.Advance(2 * time.Second)

	sweeper := startTestSweeper(t, dataStore, manualSweeperConfig())
	deleted := sweeper.RunCycle()

	if deleted != 200 {
		t.Fatalf("one cycle deleted %d keys, want all 200 because every sample was fully expired", deleted)
	}
}

func TestSweeperStopsWhenTimeBudgetIsUsed(t *testing.T) {
	clock := newFakeClock()
	dataStore := NewWithClock(clock.Now)
	fillStore(dataStore, "short", 200, time.Second)
	clock.Advance(2 * time.Second)

	config := manualSweeperConfig()
	config.MaxCycleDuration = 0
	sweeper := startTestSweeper(t, dataStore, config)

	if deleted := sweeper.RunCycle(); deleted != config.SampleSize {
		t.Fatalf("got %d deleted, want exactly one sample of %d", deleted, config.SampleSize)
	}
}

func TestSweeperLeavesFreshKeysAlone(t *testing.T) {
	clock := newFakeClock()
	dataStore := NewWithClock(clock.Now)
	fillStore(dataStore, "fresh", 50, time.Minute)

	sweeper := startTestSweeper(t, dataStore, manualSweeperConfig())
	if deleted := sweeper.RunCycle(); deleted != 0 {
		t.Fatalf("got %d deleted, want 0", deleted)
	}
	if remaining := dataStore.Len(); remaining != 50 {
		t.Fatalf("got %d keys, want 50", remaining)
	}
}

func TestBackgroundSweeperCleansUpWithRealTime(t *testing.T) {
	dataStore := New()
	fillStore(dataStore, "short", 100, 20*time.Millisecond)

	config := DefaultSweeperConfig()
	config.Interval = 5 * time.Millisecond
	startTestSweeper(t, dataStore, config)

	deadline := time.Now().Add(2 * time.Second)
	for dataStore.Len() > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("background sweeper left %d expired keys", dataStore.Len())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestStopIsSafeToCallTwice(t *testing.T) {
	sweeper := StartSweeper(New(), DefaultSweeperConfig())
	sweeper.Stop()
	sweeper.Stop()
}
