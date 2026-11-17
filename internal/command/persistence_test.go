package command

import (
	"fmt"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/LewzzJrs/redis-from-scratch/internal/resp"
	"github.com/LewzzJrs/redis-from-scratch/internal/store"
)

type recordingLog struct {
	mutex   sync.Mutex
	entries [][]string
}

func (l *recordingLog) Append(args ...string) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.entries = append(l.entries, slices.Clone(args))
	return nil
}

func (l *recordingLog) recorded() [][]string {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return slices.Clone(l.entries)
}

func newRecordingRouter() (*Router, *fakeClock, *recordingLog) {
	clock := &fakeClock{current: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	commandLog := &recordingLog{}
	return NewRouter(store.NewWithClock(clock.Now), commandLog), clock, commandLog
}

func expectRecorded(t *testing.T, commandLog *recordingLog, expected ...[]string) {
	t.Helper()
	recorded := commandLog.recorded()
	if !slices.EqualFunc(recorded, expected, slices.Equal[[]string]) {
		t.Fatalf("recorded %q, want %q", recorded, expected)
	}
}

func millisText(moment time.Time) string {
	return strconv.FormatInt(moment.UnixMilli(), 10)
}

func TestReadCommandsAreNotRecorded(t *testing.T) {
	router, _, commandLog := newRecordingRouter()

	router.Execute([]string{"PING"})
	router.Execute([]string{"ECHO", "hi"})
	router.Execute([]string{"GET", "k"})
	router.Execute([]string{"EXISTS", "k"})
	router.Execute([]string{"KEYS", "*"})
	router.Execute([]string{"DBSIZE"})

	expectRecorded(t, commandLog)
}

func TestSetIsRecordedWithAbsoluteDeadline(t *testing.T) {
	router, clock, commandLog := newRecordingRouter()
	startedAt := clock.Now()

	router.Execute([]string{"SET", "plain", "v"})
	router.Execute([]string{"SET", "seconds", "v", "EX", "10"})
	router.Execute([]string{"SET", "millis", "v", "px", "1500"})
	router.Execute([]string{"SET", "absolute", "v", "PXAT", millisText(startedAt.Add(time.Minute))})

	expectRecorded(t, commandLog,
		[]string{"SET", "plain", "v"},
		[]string{"SET", "seconds", "v", "PXAT", millisText(startedAt.Add(10 * time.Second))},
		[]string{"SET", "millis", "v", "PXAT", millisText(startedAt.Add(1500 * time.Millisecond))},
		[]string{"SET", "absolute", "v", "PXAT", millisText(startedAt.Add(time.Minute))},
	)
}

func TestIncrIsRecordedAsSetOfResult(t *testing.T) {
	router, clock, commandLog := newRecordingRouter()

	router.Execute([]string{"INCR", "visits"})
	router.Execute([]string{"INCR", "visits"})
	router.Execute([]string{"SET", "limit", "5", "EX", "60"})
	router.Execute([]string{"INCR", "limit"})

	deadline := millisText(clock.Now().Add(time.Minute))
	expectRecorded(t, commandLog,
		[]string{"SET", "visits", "1"},
		[]string{"SET", "visits", "2"},
		[]string{"SET", "limit", "5", "PXAT", deadline},
		[]string{"SET", "limit", "6", "PXAT", deadline},
	)
}

func TestDelIsRecordedOnlyWhenSomethingWasDeleted(t *testing.T) {
	router, _, commandLog := newRecordingRouter()

	router.Execute([]string{"DEL", "missing"})
	router.Execute([]string{"SET", "a", "1"})
	router.Execute([]string{"DEL", "a", "missing"})

	expectRecorded(t, commandLog,
		[]string{"SET", "a", "1"},
		[]string{"DEL", "a", "missing"},
	)
}

func TestFailedWritesAreNotRecorded(t *testing.T) {
	router, _, commandLog := newRecordingRouter()

	router.Execute([]string{"SET", "k", "v", "EX", "0"})
	router.Execute([]string{"SET", "k", "v", "BOGUS"})
	router.Execute([]string{"SET", "k"})
	router.Execute([]string{"SET", "name", "redis"})
	router.Execute([]string{"INCR", "name"})

	expectRecorded(t, commandLog, []string{"SET", "name", "redis"})
}

func TestSetWithAbsoluteDeadlineOptions(t *testing.T) {
	router, clock, _ := newRecordingRouter()
	now := clock.Now()

	expectReply(t, router, []string{"SET", "a", "1", "EXAT", strconv.FormatInt(now.Add(10*time.Second).Unix(), 10)}, resp.NewSimpleString("OK"))
	expectReply(t, router, []string{"SET", "b", "2", "PXAT", millisText(now.Add(500 * time.Millisecond))}, resp.NewSimpleString("OK"))
	expectReply(t, router, []string{"SET", "past", "3", "PXAT", millisText(now.Add(-time.Second))}, resp.NewSimpleString("OK"))

	expectReply(t, router, []string{"GET", "a"}, resp.NewBulkString("1"))
	expectReply(t, router, []string{"GET", "b"}, resp.NewBulkString("2"))
	expectReply(t, router, []string{"GET", "past"}, resp.NewNullBulkString())

	clock.Advance(time.Second)
	expectReply(t, router, []string{"GET", "b"}, resp.NewNullBulkString())
	expectReply(t, router, []string{"GET", "a"}, resp.NewBulkString("1"))

	expectReply(t, router, []string{"SET", "c", "v", "PXAT", "0"}, resp.NewError("ERR invalid expire time in 'set' command"))
	expectReply(t, router, []string{"SET", "c", "v", "EX", "10", "PXAT", "99999999999"}, resp.NewError("ERR syntax error"))
}

func TestRecordedOrderMatchesStoreOrderUnderConcurrency(t *testing.T) {
	router, _, commandLog := newRecordingRouter()

	const writerCount = 20
	const writesPerWriter = 50

	var writers sync.WaitGroup
	for writerIndex := range writerCount {
		writers.Go(func() {
			for writeIndex := range writesPerWriter {
				router.Execute([]string{"SET", "shared", fmt.Sprintf("%d-%d", writerIndex, writeIndex)})
				router.Execute([]string{"INCR", "counter"})
			}
		})
	}
	writers.Wait()

	replayed := NewRouter(store.New(), nil)
	for _, entry := range commandLog.recorded() {
		replayed.Execute(entry)
	}

	for _, key := range []string{"shared", "counter"} {
		original := router.Execute([]string{"GET", key})
		restored := replayed.Execute([]string{"GET", key})
		if original.Text != restored.Text {
			t.Fatalf("%s: replaying the log gives %q, but the live store has %q", key, restored.Text, original.Text)
		}
	}
}
