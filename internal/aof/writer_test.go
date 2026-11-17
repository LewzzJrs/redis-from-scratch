package aof

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func tempAppendOnlyFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "appendonly.aof")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func openTestWriter(t *testing.T, path string, policy FsyncPolicy) *Writer {
	t.Helper()
	writer, err := Open(path, policy)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { writer.Close() })
	return writer
}

func appendAndWait(t *testing.T, writer *Writer, args ...string) {
	t.Helper()
	sequence, err := writer.Append(args...)
	if err != nil {
		t.Fatalf("append %q: %v", args, err)
	}
	writer.WaitUntilDurable(sequence)
}

func TestParseFsyncPolicy(t *testing.T) {
	for _, valid := range []string{"always", "everysec", "no"} {
		if policy, err := ParseFsyncPolicy(valid); err != nil || string(policy) != valid {
			t.Errorf("%q: got (%q, %v)", valid, policy, err)
		}
	}
	if _, err := ParseFsyncPolicy("sometimes"); err == nil {
		t.Error("expected error for unknown policy")
	}
}

func TestDurableCommandsAreInTheFileAsResp(t *testing.T) {
	for _, policy := range []FsyncPolicy{FsyncAlways, FsyncEverySecond, FsyncNever} {
		t.Run(string(policy), func(t *testing.T) {
			path := tempAppendOnlyFile(t)
			writer := openTestWriter(t, path, policy)

			appendAndWait(t, writer, "SET", "name", "redis")
			appendAndWait(t, writer, "DEL", "name")

			expected := "*3\r\n$3\r\nSET\r\n$4\r\nname\r\n$5\r\nredis\r\n" +
				"*2\r\n$3\r\nDEL\r\n$4\r\nname\r\n"
			if content := readFile(t, path); content != expected {
				t.Fatalf("file content after WaitUntilDurable:\ngot  %q\nwant %q", content, expected)
			}
		})
	}
}

func TestSequencesIncreaseWithEveryAppend(t *testing.T) {
	writer := openTestWriter(t, tempAppendOnlyFile(t), FsyncNever)

	for expected := uint64(1); expected <= 3; expected++ {
		sequence, err := writer.Append("SET", "k", "v")
		if err != nil || sequence != expected {
			t.Fatalf("got (%d, %v), want (%d, nil)", sequence, err, expected)
		}
	}
}

func TestWaitingForSequenceZeroReturnsImmediately(t *testing.T) {
	writer := openTestWriter(t, tempAppendOnlyFile(t), FsyncAlways)

	finished := make(chan struct{})
	go func() {
		writer.WaitUntilDurable(0)
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("WaitUntilDurable(0) blocked")
	}
}

func TestValuesWithLineBreaksSurviveEncoding(t *testing.T) {
	path := tempAppendOnlyFile(t)
	writer := openTestWriter(t, path, FsyncNever)

	appendAndWait(t, writer, "SET", "multi\r\nline", "a\r\nb")

	expected := "*3\r\n$3\r\nSET\r\n$11\r\nmulti\r\nline\r\n$4\r\na\r\nb\r\n"
	if content := readFile(t, path); content != expected {
		t.Fatalf("got %q, want %q", content, expected)
	}
}

func TestCloseFlushesCommandsNobodyWaitedFor(t *testing.T) {
	path := tempAppendOnlyFile(t)
	writer, err := Open(path, FsyncEverySecond)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	for range 500 {
		writer.Append("INCR", "counter")
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if appended := strings.Count(readFile(t, path), "INCR"); appended != 500 {
		t.Fatalf("file has %d commands after close, want 500", appended)
	}
}

func TestReopeningAppendsInsteadOfTruncating(t *testing.T) {
	path := tempAppendOnlyFile(t)

	for _, value := range []string{"first", "second"} {
		writer, err := Open(path, FsyncNever)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		writer.Append("SET", "k", value)
		writer.Close()
	}

	expected := "*3\r\n$3\r\nSET\r\n$1\r\nk\r\n$5\r\nfirst\r\n" +
		"*3\r\n$3\r\nSET\r\n$1\r\nk\r\n$6\r\nsecond\r\n"
	if content := readFile(t, path); content != expected {
		t.Fatalf("got %q, want %q", content, expected)
	}
}

func TestAppendAfterCloseFails(t *testing.T) {
	writer, err := Open(tempAppendOnlyFile(t), FsyncEverySecond)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := writer.Append("SET", "k", "v"); !errors.Is(err, ErrWriterClosed) {
		t.Fatalf("got %v, want ErrWriterClosed", err)
	}
}

func TestConcurrentWritersShareBatches(t *testing.T) {
	path := tempAppendOnlyFile(t)
	writer := openTestWriter(t, path, FsyncAlways)

	const clientCount = 50
	const writesPerClient = 20

	var clients sync.WaitGroup
	for range clientCount {
		clients.Go(func() {
			for range writesPerClient {
				sequence, err := writer.Append("INCR", "counter")
				if err != nil {
					t.Errorf("append: %v", err)
					return
				}
				writer.WaitUntilDurable(sequence)
			}
		})
	}
	clients.Wait()

	totalWrites := clientCount * writesPerClient
	if written := strings.Count(readFile(t, path), "INCR"); written != totalWrites {
		t.Fatalf("file has %d commands, want %d", written, totalWrites)
	}

	writer.mutex.Lock()
	batches := writer.flushedBatchCount
	writer.mutex.Unlock()

	if batches >= uint64(totalWrites) {
		t.Fatalf("%d writes needed %d batches, want fewer batches than writes", totalWrites, batches)
	}
	t.Logf("%d durable writes from %d clients took %d fsync batches", totalWrites, clientCount, batches)
}

func TestEverySecondPolicySyncsInBackground(t *testing.T) {
	writer, err := openWithSyncInterval(tempAppendOnlyFile(t), FsyncEverySecond, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer writer.Close()

	appendAndWait(t, writer, "SET", "k", "v")

	deadline := time.Now().Add(2 * time.Second)
	for {
		writer.mutex.Lock()
		pending := writer.hasUnsyncedWrites
		writer.mutex.Unlock()

		if !pending {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("background sync never flushed the pending write")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestOpenFailsForMissingDirectory(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "does-not-exist", "appendonly.aof")
	if _, err := Open(missingPath, FsyncNever); err == nil {
		t.Fatal("expected error when the directory does not exist")
	}
}
