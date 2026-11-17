package server

import (
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/LewzzJrs/redis-from-scratch/internal/resp"
)

type testClient struct {
	t      *testing.T
	conn   net.Conn
	reader *resp.Reader
}

func startTestServer(t *testing.T) (*Server, string) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	testServer := New(listener)
	go testServer.Serve()
	t.Cleanup(func() { testServer.Close() })

	return testServer, listener.Addr().String()
}

func connectTestClient(t *testing.T) *testClient {
	t.Helper()
	_, address := startTestServer(t)
	return dialTestClient(t, address)
}

func dialTestClient(t *testing.T, address string) *testClient {
	t.Helper()

	conn, err := net.Dial("tcp", address)
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

func TestIdleClientDoesNotBlockOtherClients(t *testing.T) {
	_, address := startTestServer(t)

	idleClient := dialTestClient(t, address)
	idleClient.send("PING\r\n")
	idleClient.expectReply(resp.NewSimpleString("PONG"))

	activeClient := dialTestClient(t, address)
	activeClient.send("PING\r\n")
	activeClient.expectReply(resp.NewSimpleString("PONG"))
}

func TestManyClientsAreServedConcurrently(t *testing.T) {
	_, address := startTestServer(t)

	const clientCount = 50
	const pingsPerClient = 20

	var clients sync.WaitGroup
	failures := make(chan error, clientCount)
	for clientIndex := range clientCount {
		clients.Go(func() {
			if err := pingRepeatedly(address, clientIndex, pingsPerClient); err != nil {
				failures <- err
			}
		})
	}
	clients.Wait()
	close(failures)

	for err := range failures {
		t.Error(err)
	}
}

func pingRepeatedly(address string, clientIndex int, pingCount int) error {
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return fmt.Errorf("client %d dial: %w", clientIndex, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("client %d set deadline: %w", clientIndex, err)
	}

	reader := resp.NewReader(conn)
	writer := resp.NewWriter(conn)
	for pingIndex := range pingCount {
		message := fmt.Sprintf("client-%d-ping-%d", clientIndex, pingIndex)

		writer.Write(resp.NewCommand("PING", message))
		if err := writer.Flush(); err != nil {
			return fmt.Errorf("client %d send: %w", clientIndex, err)
		}

		reply, err := reader.ReadValue()
		if err != nil {
			return fmt.Errorf("client %d read: %w", clientIndex, err)
		}
		if reply.Type != resp.BulkString || reply.Text != message {
			return fmt.Errorf("client %d got %#v, want bulk string %q", clientIndex, reply, message)
		}
	}
	return nil
}

func TestCloseDisconnectsClientsAndStopsServe(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	testServer := New(listener)

	serveResult := make(chan error, 1)
	go func() {
		serveResult <- testServer.Serve()
	}()

	client := dialTestClient(t, listener.Addr().String())
	client.send("PING\r\n")
	client.expectReply(resp.NewSimpleString("PONG"))

	if connected := testServer.ConnectedClients(); connected != 1 {
		t.Fatalf("got %d connected clients before close, want 1", connected)
	}

	if err := testServer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case err := <-serveResult:
		if err != nil {
			t.Fatalf("Serve returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after Close")
	}

	if _, err := client.reader.ReadValue(); err == nil {
		t.Fatal("expected client connection to be closed by server")
	}
	if connected := testServer.ConnectedClients(); connected != 0 {
		t.Fatalf("got %d connected clients after close, want 0", connected)
	}
}
