package main

import (
	"flag"
	"log"
	"net"
	"strconv"

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
	if err := redisServer.Serve(); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
