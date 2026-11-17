package store

import (
	"fmt"
	"sync"
	"testing"
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
