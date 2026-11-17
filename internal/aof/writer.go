package aof

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/LewzzJrs/redis-from-scratch/internal/resp"
)

type FsyncPolicy string

const (
	FsyncAlways      FsyncPolicy = "always"
	FsyncEverySecond FsyncPolicy = "everysec"
	FsyncNever       FsyncPolicy = "no"
)

const defaultSyncInterval = time.Second

var ErrWriterClosed = errors.New("aof writer is closed")

func ParseFsyncPolicy(text string) (FsyncPolicy, error) {
	switch policy := FsyncPolicy(text); policy {
	case FsyncAlways, FsyncEverySecond, FsyncNever:
		return policy, nil
	default:
		return "", fmt.Errorf("unknown fsync policy %q, expected always, everysec, or no", text)
	}
}

type Writer struct {
	mutex             sync.Mutex
	file              *os.File
	policy            FsyncPolicy
	encodedCommand    bytes.Buffer
	encoder           *resp.Writer
	hasUnsyncedWrites bool

	stopSyncing     chan struct{}
	syncingFinished chan struct{}
	closeOnce       sync.Once
	closeError      error
}

func Open(path string, policy FsyncPolicy) (*Writer, error) {
	return openWithSyncInterval(path, policy, defaultSyncInterval)
}

func openWithSyncInterval(path string, policy FsyncPolicy, syncInterval time.Duration) (*Writer, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open aof file: %w", err)
	}

	writer := &Writer{
		file:            file,
		policy:          policy,
		stopSyncing:     make(chan struct{}),
		syncingFinished: make(chan struct{}),
	}
	writer.encoder = resp.NewWriter(&writer.encodedCommand)

	if policy == FsyncEverySecond {
		go writer.syncPeriodically(syncInterval)
	} else {
		close(writer.syncingFinished)
	}
	return writer, nil
}

func (w *Writer) Append(args ...string) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.file == nil {
		return ErrWriterClosed
	}

	w.encodedCommand.Reset()
	w.encoder.Write(resp.NewCommand(args...))
	if err := w.encoder.Flush(); err != nil {
		return fmt.Errorf("encode command: %w", err)
	}

	if _, err := w.file.Write(w.encodedCommand.Bytes()); err != nil {
		return fmt.Errorf("write aof file: %w", err)
	}

	switch w.policy {
	case FsyncAlways:
		return w.file.Sync()
	case FsyncEverySecond:
		w.hasUnsyncedWrites = true
	}
	return nil
}

func (w *Writer) Sync() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.syncLocked()
}

func (w *Writer) Close() error {
	w.closeOnce.Do(func() {
		close(w.stopSyncing)
		<-w.syncingFinished

		w.mutex.Lock()
		defer w.mutex.Unlock()

		syncError := w.file.Sync()
		closeError := w.file.Close()
		w.file = nil
		w.closeError = errors.Join(syncError, closeError)
	})
	return w.closeError
}

func (w *Writer) syncLocked() error {
	if w.file == nil || !w.hasUnsyncedWrites {
		return nil
	}
	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("fsync aof file: %w", err)
	}
	w.hasUnsyncedWrites = false
	return nil
}

func (w *Writer) syncPeriodically(interval time.Duration) {
	defer close(w.syncingFinished)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopSyncing:
			return
		case <-ticker.C:
			if err := w.Sync(); err != nil {
				log.Printf("background aof fsync failed: %v", err)
			}
		}
	}
}
