package command

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/LewzzJrs/redis-from-scratch/internal/resp"
	"github.com/LewzzJrs/redis-from-scratch/internal/store"
)

func newTestRouter() *Router {
	return NewRouter(store.New())
}

func expectReply(t *testing.T, router *Router, args []string, expected resp.Value) {
	t.Helper()
	if reply := router.Execute(args); !reflect.DeepEqual(reply, expected) {
		t.Fatalf("%q: got %#v, want %#v", args, reply, expected)
	}
}

func TestPing(t *testing.T) {
	router := newTestRouter()

	expectReply(t, router, []string{"PING"}, resp.NewSimpleString("PONG"))
	expectReply(t, router, []string{"PING", "hello"}, resp.NewBulkString("hello"))
	expectReply(t, router, []string{"PING", "a", "b"}, resp.NewError("ERR wrong number of arguments for 'ping' command"))
}

func TestEcho(t *testing.T) {
	router := newTestRouter()

	expectReply(t, router, []string{"ECHO", "hello world"}, resp.NewBulkString("hello world"))
	expectReply(t, router, []string{"ECHO", ""}, resp.NewBulkString(""))
	expectReply(t, router, []string{"ECHO"}, resp.NewError("ERR wrong number of arguments for 'echo' command"))
	expectReply(t, router, []string{"ECHO", "a", "b"}, resp.NewError("ERR wrong number of arguments for 'echo' command"))
}

func TestSetThenGet(t *testing.T) {
	router := newTestRouter()

	expectReply(t, router, []string{"SET", "name", "redis"}, resp.NewSimpleString("OK"))
	expectReply(t, router, []string{"GET", "name"}, resp.NewBulkString("redis"))

	expectReply(t, router, []string{"SET", "name", "go"}, resp.NewSimpleString("OK"))
	expectReply(t, router, []string{"GET", "name"}, resp.NewBulkString("go"))
}

func TestGetMissingKeyReturnsNull(t *testing.T) {
	expectReply(t, newTestRouter(), []string{"GET", "missing"}, resp.NewNullBulkString())
}

func TestSetAndGetArgumentErrors(t *testing.T) {
	router := newTestRouter()

	expectReply(t, router, []string{"SET", "key"}, resp.NewError("ERR wrong number of arguments for 'set' command"))
	expectReply(t, router, []string{"GET"}, resp.NewError("ERR wrong number of arguments for 'get' command"))
	expectReply(t, router, []string{"GET", "a", "b"}, resp.NewError("ERR wrong number of arguments for 'get' command"))
}

func TestCommandNamesAreCaseInsensitive(t *testing.T) {
	router := newTestRouter()

	expectReply(t, router, []string{"set", "k", "v"}, resp.NewSimpleString("OK"))
	expectReply(t, router, []string{"GeT", "k"}, resp.NewBulkString("v"))
	expectReply(t, router, []string{"echo", "x"}, resp.NewBulkString("x"))
}

func TestKeysAreCaseSensitive(t *testing.T) {
	router := newTestRouter()

	expectReply(t, router, []string{"SET", "Key", "upper"}, resp.NewSimpleString("OK"))
	expectReply(t, router, []string{"GET", "key"}, resp.NewNullBulkString())
}

func TestUnknownCommand(t *testing.T) {
	expectReply(t, newTestRouter(), []string{"FOO", "a", "b"},
		resp.NewError("ERR unknown command 'FOO', with args beginning with: 'a' 'b' "))
}

func TestEmptyCommand(t *testing.T) {
	expectReply(t, newTestRouter(), []string{}, resp.NewError("ERR empty command"))
}

type fakeClock struct {
	mutex   sync.Mutex
	current time.Time
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

func newRouterWithFakeClock() (*Router, *fakeClock) {
	clock := &fakeClock{current: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	return NewRouter(store.NewWithClock(clock.Now)), clock
}

func TestSetWithExExpiresAfterSeconds(t *testing.T) {
	router, clock := newRouterWithFakeClock()

	expectReply(t, router, []string{"SET", "session", "abc", "EX", "10"}, resp.NewSimpleString("OK"))
	clock.Advance(10 * time.Second)
	expectReply(t, router, []string{"GET", "session"}, resp.NewBulkString("abc"))

	clock.Advance(time.Millisecond)
	expectReply(t, router, []string{"GET", "session"}, resp.NewNullBulkString())
}

func TestSetWithPxExpiresAfterMilliseconds(t *testing.T) {
	router, clock := newRouterWithFakeClock()

	expectReply(t, router, []string{"SET", "lock", "owner-1", "PX", "1500"}, resp.NewSimpleString("OK"))
	clock.Advance(1500 * time.Millisecond)
	expectReply(t, router, []string{"GET", "lock"}, resp.NewBulkString("owner-1"))

	clock.Advance(time.Millisecond)
	expectReply(t, router, []string{"GET", "lock"}, resp.NewNullBulkString())
}

func TestExpiryOptionsAreCaseInsensitive(t *testing.T) {
	router, clock := newRouterWithFakeClock()

	expectReply(t, router, []string{"SET", "a", "1", "ex", "1"}, resp.NewSimpleString("OK"))
	expectReply(t, router, []string{"SET", "b", "2", "Px", "1000"}, resp.NewSimpleString("OK"))
	clock.Advance(2 * time.Second)

	expectReply(t, router, []string{"GET", "a"}, resp.NewNullBulkString())
	expectReply(t, router, []string{"GET", "b"}, resp.NewNullBulkString())
}

func TestSetWithoutExpiryClearsPreviousExpiry(t *testing.T) {
	router, clock := newRouterWithFakeClock()

	expectReply(t, router, []string{"SET", "k", "temporary", "EX", "1"}, resp.NewSimpleString("OK"))
	expectReply(t, router, []string{"SET", "k", "permanent"}, resp.NewSimpleString("OK"))
	clock.Advance(time.Hour)

	expectReply(t, router, []string{"GET", "k"}, resp.NewBulkString("permanent"))
}

func TestSetExpiryOptionErrors(t *testing.T) {
	notAnInteger := resp.NewError("ERR value is not an integer or out of range")
	invalidExpireTime := resp.NewError("ERR invalid expire time in 'set' command")
	syntaxError := resp.NewError("ERR syntax error")

	testCases := []struct {
		name     string
		options  []string
		expected resp.Value
	}{
		{"EX without amount", []string{"EX"}, syntaxError},
		{"unknown option", []string{"BOGUS", "1"}, syntaxError},
		{"EX and PX together", []string{"EX", "10", "PX", "100"}, syntaxError},
		{"EX twice", []string{"EX", "10", "EX", "20"}, syntaxError},
		{"non numeric amount", []string{"EX", "ten"}, notAnInteger},
		{"decimal amount", []string{"PX", "1.5"}, notAnInteger},
		{"zero seconds", []string{"EX", "0"}, invalidExpireTime},
		{"negative milliseconds", []string{"PX", "-5"}, invalidExpireTime},
		{"seconds overflow", []string{"EX", "9223372036854775807"}, invalidExpireTime},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			router := newTestRouter()
			args := append([]string{"SET", "k", "v"}, testCase.options...)

			expectReply(t, router, args, testCase.expected)
			expectReply(t, router, []string{"GET", "k"}, resp.NewNullBulkString())
		})
	}
}

func TestDbsizeCountsKeysStillInMemory(t *testing.T) {
	router, clock := newRouterWithFakeClock()

	expectReply(t, router, []string{"DBSIZE"}, resp.NewInteger(0))
	expectReply(t, router, []string{"SET", "a", "1"}, resp.NewSimpleString("OK"))
	expectReply(t, router, []string{"SET", "b", "2", "PX", "100"}, resp.NewSimpleString("OK"))
	expectReply(t, router, []string{"DBSIZE"}, resp.NewInteger(2))

	clock.Advance(time.Second)
	expectReply(t, router, []string{"DBSIZE"}, resp.NewInteger(2))

	expectReply(t, router, []string{"GET", "b"}, resp.NewNullBulkString())
	expectReply(t, router, []string{"DBSIZE"}, resp.NewInteger(1))

	expectReply(t, router, []string{"DBSIZE", "extra"}, resp.NewError("ERR wrong number of arguments for 'dbsize' command"))
}
