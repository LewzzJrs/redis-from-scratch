package server

import (
	"errors"
	"io"
	"log"
	"net"

	"github.com/LewzzJrs/redis-from-scratch/internal/resp"
)

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

		if err := writer.Write(executeCommand(args)); err != nil {
			log.Printf("write to %s failed: %v", conn.RemoteAddr(), err)
			return
		}

		if reader.Buffered() == 0 {
			if err := writer.Flush(); err != nil {
				log.Printf("flush to %s failed: %v", conn.RemoteAddr(), err)
				return
			}
		}
	}
}

func (s *Server) handleReadError(conn net.Conn, writer *resp.Writer, err error) {
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return
	}

	var protocolError *resp.ProtocolError
	if errors.As(err, &protocolError) {
		writer.Write(resp.NewError("ERR Protocol error: " + protocolError.Reason))
		writer.Flush()
		return
	}

	log.Printf("read from %s failed: %v", conn.RemoteAddr(), err)
}
