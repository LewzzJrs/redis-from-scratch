package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/LewzzJrs/redis-from-scratch/internal/server"
)

func main() {
	port := flag.Int("port", 6379, "TCP port to listen on")
	flag.Parse()

	address := net.JoinHostPort("", strconv.Itoa(*port))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", address, err)
	}
	log.Printf("listening on %s", listener.Addr())

	redisServer := server.New(listener)

	shutdownSignal, stopListeningForSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopListeningForSignals()

	serveResult := make(chan error, 1)
	go func() {
		serveResult <- redisServer.Serve()
	}()

	select {
	case <-shutdownSignal.Done():
		log.Printf("shutting down, closing %d client connection(s)", redisServer.ConnectedClients())
		redisServer.Close()
		log.Println("server stopped")
	case err := <-serveResult:
		if err != nil {
			log.Fatalf("server stopped unexpectedly: %v", err)
		}
	}
}
