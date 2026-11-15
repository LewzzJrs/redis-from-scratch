package server

import (
	"errors"
	"io"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/LewzzJrs/redis-from-scratch/internal/resp"
)

type testClient struct {
	t      *testing.T
	conn   net.Conn
	reader *resp.Reader
}

func startTestServer(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	testServer := New(listener)
	go testServer.Serve()
	t.Cleanup(func() { testServer.Close() })

	return listener.Addr().String()
}

func connectTestClient(t *testing.T) *testClient {
	t.Helper()

	conn, err := net.Dial("tcp", startTestServer(t))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	return &testClient{t: t, conn: conn, reader: resp.NewReader(conn)}
}

func (c *testClient) send(rawRequest string) {
	c.t.Helper()
	if _, err := io.WriteString(c.conn, rawRequest); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

func (c *testClient) expectReply(expected resp.Value) {
	c.t.Helper()
	reply, err := c.reader.ReadValue()
	if err != nil {
		c.t.Fatalf("read reply: %v", err)
	}
	if !reflect.DeepEqual(reply, expected) {
		c.t.Fatalf("got %#v, want %#v", reply, expected)
	}
}

func TestPingRespondsWithPong(t *testing.T) {
	requests := map[string]string{
		"resp array":       "*1\r\n$4\r\nPING\r\n",
		"inline uppercase": "PING\r\n",
		"inline lowercase": "ping\n",
	}

	for name, request := range requests {
		t.Run(name, func(t *testing.T) {
			client := connectTestClient(t)
			client.send(request)
			client.expectReply(resp.NewSimpleString("PONG"))
		})
	}
}

func TestPingWithMessageEchoesMessage(t *testing.T) {
	client := connectTestClient(t)
	client.send("*2\r\n$4\r\nPING\r\n$11\r\nhello world\r\n")
	client.expectReply(resp.NewBulkString("hello world"))
}

func TestPingWithTooManyArgumentsReturnsError(t *testing.T) {
	client := connectTestClient(t)
	client.send("PING a b\r\n")
	client.expectReply(resp.NewError("ERR wrong number of arguments for 'ping' command"))
}

func TestPipelinedCommandsGetOneReplyEach(t *testing.T) {
	client := connectTestClient(t)
	client.send("*1\r\n$4\r\nPING\r\n*1\r\n$4\r\nPING\r\n*2\r\n$4\r\nPING\r\n$2\r\nhi\r\n")

	client.expectReply(resp.NewSimpleString("PONG"))
	client.expectReply(resp.NewSimpleString("PONG"))
	client.expectReply(resp.NewBulkString("hi"))
}

func TestEmptyLinesAreIgnored(t *testing.T) {
	client := connectTestClient(t)
	client.send("\r\n\r\nPING\r\n")
	client.expectReply(resp.NewSimpleString("PONG"))
}

func TestUnknownCommandReturnsError(t *testing.T) {
	client := connectTestClient(t)
	client.send("*3\r\n$5\r\nHELLO\r\n$1\r\na\r\n$1\r\nb\r\n")
	client.expectReply(resp.NewError("ERR unknown command 'HELLO', with args beginning with: 'a' 'b' "))
}

func TestProtocolErrorRepliesAndClosesConnection(t *testing.T) {
	client := connectTestClient(t)
	client.send("*1\r\n$abc\r\n")
	client.expectReply(resp.NewError(`ERR Protocol error: invalid bulk length "abc"`))

	if _, err := client.reader.ReadValue(); !errors.Is(err, io.EOF) {
		t.Fatalf("got %v, want connection closed (io.EOF)", err)
	}
}
