package aof

import (
	"errors"
	"os"
	"path/filepath"
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

func TestAppendWritesCommandsAsResp(t *testing.T) {
	for _, policy := range []FsyncPolicy{FsyncAlways, FsyncEverySecond, FsyncNever} {
		t.Run(string(policy), func(t *testing.T) {
			path := tempAppendOnlyFile(t)
			writer, err := Open(path, policy)
			if err != nil {
				t.Fatalf("open: %v", err)
			}

			if err := writer.Append("SET", "name", "redis"); err != nil {
				t.Fatalf("append: %v", err)
			}
			if err := writer.Append("DEL", "name"); err != nil {
				t.Fatalf("append: %v", err)
			}

			expected := "*3\r\n$3\r\nSET\r\n$4\r\nname\r\n$5\r\nredis\r\n" +
				"*2\r\n$3\r\nDEL\r\n$4\r\nname\r\n"
			if content := readFile(t, path); content != expected {
				t.Fatalf("file content before close:\ngot  %q\nwant %q", content, expected)
			}

			if err := writer.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}
		})
	}
}

func TestValuesWithLineBreaksSurviveEncoding(t *testing.T) {
	path := tempAppendOnlyFile(t)
	writer, err := Open(path, FsyncNever)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer writer.Close()

	writer.Append("SET", "multi\r\nline", "a\r\nb")

	expected := "*3\r\n$3\r\nSET\r\n$11\r\nmulti\r\nline\r\n$4\r\na\r\nb\r\n"
	if content := readFile(t, path); content != expected {
		t.Fatalf("got %q, want %q", content, expected)
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
	if err := writer.Append("SET", "k", "v"); !errors.Is(err, ErrWriterClosed) {
		t.Fatalf("got %v, want ErrWriterClosed", err)
	}
}

func TestEverySecondPolicySyncsInBackground(t *testing.T) {
	writer, err := openWithSyncInterval(tempAppendOnlyFile(t), FsyncEverySecond, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer writer.Close()

	writer.Append("SET", "k", "v")

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
