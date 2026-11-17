package command

import (
	"reflect"
	"testing"

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
	expectReply(t, router, []string{"SET", "key", "value", "BOGUS"}, resp.NewError("ERR syntax error"))
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
