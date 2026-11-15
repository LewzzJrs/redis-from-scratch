package server

import (
	"bufio"
	"errors"
	"io"
	"log"
	"net"
	"strings"
)

const pongReply = "+PONG\r\n"

type Server struct {
	listener net.Listener
}

func New(listener net.Listener) *Server {
	return &Server{listener: listener}
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
		s.handleConnection(conn)
	}
}

func (s *Server) Close() error {
	return s.listener.Close()
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("read from %s failed: %v", conn.RemoteAddr(), err)
			}
			return
		}

		if isPingCommand(line) {
			if _, err := io.WriteString(conn, pongReply); err != nil {
				log.Printf("write to %s failed: %v", conn.RemoteAddr(), err)
				return
			}
		}
	}
}

func isPingCommand(line string) bool {
	return strings.EqualFold(strings.TrimSpace(line), "PING")
}
