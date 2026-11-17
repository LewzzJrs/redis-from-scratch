package server

import (
	"errors"
	"io"
	"log"
	"net"
	"sync"

	"github.com/LewzzJrs/redis-from-scratch/internal/resp"
)

type CommandExecutor interface {
	Execute(args []string) resp.Value
}

type Server struct {
	listener net.Listener
	executor CommandExecutor

	mutex       sync.Mutex
	connections map[net.Conn]struct{}
	closing     bool
	handlers    sync.WaitGroup
}

func New(listener net.Listener, executor CommandExecutor) *Server {
	return &Server{
		listener:    listener,
		executor:    executor,
		connections: make(map[net.Conn]struct{}),
	}
}

func (s *Server) Serve() error {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}

		if !s.trackConnection(conn) {
			conn.Close()
			continue
		}

		go func() {
			defer s.handlers.Done()
			defer s.untrackConnection(conn)
			s.handleConnection(conn)
		}()
	}
}

func (s *Server) Close() error {
	s.mutex.Lock()
	s.closing = true
	listenerError := s.listener.Close()
	for conn := range s.connections {
		conn.Close()
	}
	s.mutex.Unlock()

	s.handlers.Wait()
	return listenerError
}

func (s *Server) ConnectedClients() int {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return len(s.connections)
}

func (s *Server) trackConnection(conn net.Conn) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.closing {
		return false
	}
	s.connections[conn] = struct{}{}
	s.handlers.Add(1)
	return true
}

func (s *Server) untrackConnection(conn net.Conn) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.connections, conn)
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	reader := resp.NewReader(conn)
	writer := resp.NewWriter(conn)

	for {
		args, err := reader.ReadCommand()
		if err != nil {
			s.handleReadError(conn, writer, err)
			return
		}
		if len(args) == 0 {
			continue
		}

		if err := writer.Write(s.executor.Execute(args)); err != nil {
			logConnectionError(conn, "write", err)
			return
		}

		if reader.Buffered() == 0 {
			if err := writer.Flush(); err != nil {
				logConnectionError(conn, "flush", err)
				return
			}
		}
	}
}

func logConnectionError(conn net.Conn, action string, err error) {
	if errors.Is(err, net.ErrClosed) {
		return
	}
	log.Printf("%s %s failed: %v", action, conn.RemoteAddr(), err)
}

func (s *Server) handleReadError(conn net.Conn, writer *resp.Writer, err error) {
	if errors.Is(err, io.EOF) {
		return
	}

	var protocolError *resp.ProtocolError
	if errors.As(err, &protocolError) {
		writer.Write(resp.NewError("ERR Protocol error: " + protocolError.Reason))
		writer.Flush()
		return
	}

	logConnectionError(conn, "read", err)
}
