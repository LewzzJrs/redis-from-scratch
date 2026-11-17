package aof

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/LewzzJrs/redis-from-scratch/internal/command"
	"github.com/LewzzJrs/redis-from-scratch/internal/resp"
	"github.com/LewzzJrs/redis-from-scratch/internal/store"
)

func encodeCommands(commands ...[]string) string {
	var encoded string
	for _, args := range commands {
		encoded += "*" + strconv.Itoa(len(args)) + "\r\n"
		for _, arg := range args {
			encoded += "$" + strconv.Itoa(len(arg)) + "\r\n" + arg + "\r\n"
		}
	}
	return encoded
}

func writeAppendOnlyFile(t *testing.T, content string) string {
	t.Helper()
	path := tempAppendOnlyFile(t)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func loadIntoNewStore(t *testing.T, path string) (*store.Store, *command.Router, LoadResult, error) {
	t.Helper()
	dataStore := store.New()
	router := command.NewRouter(dataStore, nil)
	result, err := Load(path, router)
	return dataStore, router, result, err
}

func expectValue(t *testing.T, router *command.Router, key string, expected resp.Value) {
	t.Helper()
	reply := router.Execute([]string{"GET", key})
	if reply.Type != expected.Type || reply.Text != expected.Text || reply.IsNull != expected.IsNull {
		t.Fatalf("GET %s: got %#v, want %#v", key, reply, expected)
	}
}

func TestLoadMissingFileStartsEmpty(t *testing.T) {
	dataStore, _, result, err := loadIntoNewStore(t, filepath.Join(t.TempDir(), "missing.aof"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CommandsReplayed != 0 || dataStore.Len() != 0 {
		t.Fatalf("got %+v with %d keys, want empty", result, dataStore.Len())
	}
}

func TestLoadEmptyFile(t *testing.T) {
	_, _, result, err := loadIntoNewStore(t, writeAppendOnlyFile(t, ""))
	if err != nil || result != (LoadResult{}) {
		t.Fatalf("got (%+v, %v), want empty result", result, err)
	}
}

func TestLoadReplaysCommandsInOrder(t *testing.T) {
	future := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	past := strconv.FormatInt(time.Now().Add(-time.Hour).UnixMilli(), 10)

	path := writeAppendOnlyFile(t, encodeCommands(
		[]string{"SET", "name", "first"},
		[]string{"SET", "name", "second"},
		[]string{"SET", "session", "abc", "PXAT", future},
		[]string{"SET", "otp", "123456", "PXAT", past},
		[]string{"SET", "deleted", "x"},
		[]string{"DEL", "deleted"},
		[]string{"SET", "visits", "2"},
	))

	dataStore, router, result, err := loadIntoNewStore(t, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CommandsReplayed != 7 || result.TruncatedBytes != 0 {
		t.Fatalf("got %+v, want 7 commands and no truncation", result)
	}

	expectValue(t, router, "name", resp.NewBulkString("second"))
	expectValue(t, router, "session", resp.NewBulkString("abc"))
	expectValue(t, router, "otp", resp.NewNullBulkString())
	expectValue(t, router, "deleted", resp.NewNullBulkString())
	expectValue(t, router, "visits", resp.NewBulkString("2"))

	if keyCount := dataStore.Len(); keyCount != 3 {
		t.Fatalf("got %d keys, want 3 (name, session, visits)", keyCount)
	}
}

func TestLoadDoesNotWriteBackToTheFile(t *testing.T) {
	content := encodeCommands([]string{"SET", "a", "1"}, []string{"SET", "b", "2"})
	path := writeAppendOnlyFile(t, content)

	if _, _, _, err := loadIntoNewStore(t, path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if after := readFile(t, path); after != content {
		t.Fatalf("file changed during load:\ngot  %q\nwant %q", after, content)
	}
}

func TestLoadTruncatesIncompleteLastCommand(t *testing.T) {
	completeCommand := encodeCommands([]string{"SET", "kept", "yes"})
	interruptedCommand := encodeCommands([]string{"SET", "lost", "value"})

	for cutLength := 1; cutLength < len(interruptedCommand); cutLength++ {
		path := writeAppendOnlyFile(t, completeCommand+interruptedCommand[:cutLength])

		_, router, result, err := loadIntoNewStore(t, path)
		if err != nil {
			t.Fatalf("cut at %d: unexpected error: %v", cutLength, err)
		}
		if result.CommandsReplayed != 1 || result.TruncatedBytes != int64(cutLength) {
			t.Fatalf("cut at %d: got %+v, want 1 command and %d truncated bytes", cutLength, result, cutLength)
		}
		if after := readFile(t, path); after != completeCommand {
			t.Fatalf("cut at %d: file not truncated to last complete command, got %q", cutLength, after)
		}
		expectValue(t, router, "kept", resp.NewBulkString("yes"))
		expectValue(t, router, "lost", resp.NewNullBulkString())
	}
}

func TestAppendingAfterTruncationKeepsFileValid(t *testing.T) {
	path := writeAppendOnlyFile(t, encodeCommands([]string{"SET", "a", "1"})+"*3\r\n$3\r\nSET\r\n$1\r\nb")

	if _, _, _, err := loadIntoNewStore(t, path); err != nil {
		t.Fatalf("first load: %v", err)
	}

	writer, err := Open(path, FsyncNever)
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	writer.Append("SET", "c", "3")
	writer.Close()

	_, router, result, err := loadIntoNewStore(t, path)
	if err != nil || result.CommandsReplayed != 2 || result.TruncatedBytes != 0 {
		t.Fatalf("second load: got (%+v, %v), want 2 clean commands", result, err)
	}
	expectValue(t, router, "a", resp.NewBulkString("1"))
	expectValue(t, router, "c", resp.NewBulkString("3"))
}

func TestLoadRefusesCorruptionInTheMiddle(t *testing.T) {
	testCases := map[string]string{
		"broken bulk length": encodeCommands([]string{"SET", "a", "1"}) + "*3\r\n$X\r\nSET\r\n" + encodeCommands([]string{"SET", "b", "2"}),
		"garbage line":       encodeCommands([]string{"SET", "a", "1"}) + "this is not resp\r\n" + encodeCommands([]string{"SET", "b", "2"}),
		"unknown command":    encodeCommands([]string{"SET", "a", "1"}, []string{"FLUSHEVERYTHING"}, []string{"SET", "b", "2"}),
		"invalid arguments":  encodeCommands([]string{"SET", "a", "1"}, []string{"SET", "b"}, []string{"SET", "c", "3"}),
	}

	for name, content := range testCases {
		t.Run(name, func(t *testing.T) {
			path := writeAppendOnlyFile(t, content)

			_, _, _, err := loadIntoNewStore(t, path)
			if !errors.Is(err, ErrCorrupted) {
				t.Fatalf("got %v, want ErrCorrupted", err)
			}
			if after := readFile(t, path); after != content {
				t.Fatal("a corrupted file must be left untouched for a human to inspect")
			}
		})
	}
}

func TestDataSurvivesRestart(t *testing.T) {
	path := tempAppendOnlyFile(t)

	writer, err := Open(path, FsyncAlways)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	liveRouter := command.NewRouter(store.New(), writer)
	for _, args := range [][]string{
		{"SET", "name", "Lewi"},
		{"SET", "session", "abc", "EX", "3600"},
		{"SET", "otp", "123", "PX", "1"},
		{"INCR", "visits"},
		{"INCR", "visits"},
		{"INCR", "visits"},
		{"SET", "temp", "x"},
		{"DEL", "temp"},
	} {
		liveRouter.Execute(args)
	}
	writer.Close()
	time.Sleep(5 * time.Millisecond)

	_, restartedRouter, _, err := loadIntoNewStore(t, path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	expectValue(t, restartedRouter, "name", resp.NewBulkString("Lewi"))
	expectValue(t, restartedRouter, "session", resp.NewBulkString("abc"))
	expectValue(t, restartedRouter, "otp", resp.NewNullBulkString())
	expectValue(t, restartedRouter, "visits", resp.NewBulkString("3"))
	expectValue(t, restartedRouter, "temp", resp.NewNullBulkString())
}
