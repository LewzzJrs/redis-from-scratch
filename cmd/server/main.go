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

	"github.com/LewzzJrs/redis-from-scratch/internal/aof"
	"github.com/LewzzJrs/redis-from-scratch/internal/command"
	"github.com/LewzzJrs/redis-from-scratch/internal/server"
	"github.com/LewzzJrs/redis-from-scratch/internal/store"
)

func main() {
	port := flag.Int("port", 6379, "TCP port to listen on")
	aofPath := flag.String("aof", "appendonly.aof", "path of the append-only file, empty to disable persistence")
	fsyncPolicyText := flag.String("appendfsync", string(aof.FsyncEverySecond), "when to fsync the append-only file: always, everysec, or no")
	flag.Parse()

	var commandLog command.CommandLog
	if *aofPath != "" {
		fsyncPolicy, err := aof.ParseFsyncPolicy(*fsyncPolicyText)
		if err != nil {
			log.Fatal(err)
		}

		aofWriter, err := aof.Open(*aofPath, fsyncPolicy)
		if err != nil {
			log.Fatal(err)
		}
		defer closeAppendOnlyFile(aofWriter)

		commandLog = aofWriter
		log.Printf("appending writes to %s (appendfsync %s)", *aofPath, fsyncPolicy)
	} else {
		log.Println("persistence disabled, data lives only in memory")
	}

	address := net.JoinHostPort("", strconv.Itoa(*port))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", address, err)
	}
	log.Printf("listening on %s", listener.Addr())

	dataStore := store.New()
	expirySweeper := store.StartSweeper(dataStore, store.DefaultSweeperConfig())
	defer expirySweeper.Stop()

	commandRouter := command.NewRouter(dataStore, commandLog)
	redisServer := server.New(listener, commandRouter)

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

func closeAppendOnlyFile(aofWriter *aof.Writer) {
	if err := aofWriter.Close(); err != nil {
		log.Printf("failed to close append-only file: %v", err)
		return
	}
	log.Println("append-only file flushed and closed")
}
