package server

import (
	"bufio"
	"io"
	"net"
	"testing"
	"time"
)

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

func dialTestServer(t *testing.T, address string) (net.Conn, *bufio.Reader) {
	t.Helper()

	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	return conn, bufio.NewReader(conn)
}

func TestPingRespondsWithPong(t *testing.T) {
	requests := map[string]string{
		"resp array":       "*1\r\n$4\r\nPING\r\n",
		"inline uppercase": "PING\r\n",
		"inline lowercase": "ping\n",
	}

	for name, request := range requests {
		t.Run(name, func(t *testing.T) {
			address := startTestServer(t)
			conn, reader := dialTestServer(t, address)

			if _, err := io.WriteString(conn, request); err != nil {
				t.Fatalf("write: %v", err)
			}

			reply, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if reply != pongReply {
				t.Fatalf("got %q, want %q", reply, pongReply)
			}
		})
	}
}

func TestPipelinedPingsGetOneReplyEach(t *testing.T) {
	address := startTestServer(t)
	conn, reader := dialTestServer(t, address)

	pipelinedPings := "*1\r\n$4\r\nPING\r\n*1\r\n$4\r\nPING\r\n*1\r\n$4\r\nPING\r\n"
	if _, err := io.WriteString(conn, pipelinedPings); err != nil {
		t.Fatalf("write: %v", err)
	}

	for replyIndex := range 3 {
		reply, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read reply %d: %v", replyIndex, err)
		}
		if reply != pongReply {
			t.Fatalf("reply %d: got %q, want %q", replyIndex, reply, pongReply)
		}
	}
}
