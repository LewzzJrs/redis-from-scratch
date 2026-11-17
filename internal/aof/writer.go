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
	file   *os.File
	policy FsyncPolicy

	mutex             sync.Mutex
	batchReady        *sync.Cond
	batchDurable      *sync.Cond
	pending           bytes.Buffer
	encoder           *resp.Writer
	appendedSequence  uint64
	durableSequence   uint64
	flushedBatchCount uint64
	hasUnsyncedWrites bool
	closing           bool

	flusherFinished chan struct{}
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
		flusherFinished: make(chan struct{}),
		stopSyncing:     make(chan struct{}),
		syncingFinished: make(chan struct{}),
	}
	writer.batchReady = sync.NewCond(&writer.mutex)
	writer.batchDurable = sync.NewCond(&writer.mutex)
	writer.encoder = resp.NewWriter(&writer.pending)

	go writer.flushBatches()
	if policy == FsyncEverySecond {
		go writer.syncPeriodically(syncInterval)
	} else {
		close(writer.syncingFinished)
	}
	return writer, nil
}

func (w *Writer) Append(args ...string) (uint64, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.closing {
		return 0, ErrWriterClosed
	}

	w.encoder.Write(resp.NewCommand(args...))
	if err := w.encoder.Flush(); err != nil {
		return 0, fmt.Errorf("encode command: %w", err)
	}

	w.appendedSequence++
	w.batchReady.Signal()
	return w.appendedSequence, nil
}

func (w *Writer) WaitUntilDurable(sequence uint64) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	for w.durableSequence < sequence {
		w.batchDurable.Wait()
	}
}

func (w *Writer) Close() error {
	w.closeOnce.Do(func() {
		w.mutex.Lock()
		w.closing = true
		w.batchReady.Broadcast()
		w.mutex.Unlock()

		<-w.flusherFinished
		close(w.stopSyncing)
		<-w.syncingFinished

		w.closeError = errors.Join(w.file.Sync(), w.file.Close())
	})
	return w.closeError
}

func (w *Writer) flushBatches() {
	defer close(w.flusherFinished)

	var batch []byte
	for {
		w.mutex.Lock()
		for w.pending.Len() == 0 && !w.closing {
			w.batchReady.Wait()
		}
		if w.pending.Len() == 0 {
			w.mutex.Unlock()
			return
		}

		batch = append(batch[:0], w.pending.Bytes()...)
		w.pending.Reset()
		lastSequenceInBatch := w.appendedSequence
		w.mutex.Unlock()

		if err := w.persistBatch(batch); err != nil {
			log.Printf("aof batch of %d byte(s) was not persisted: %v", len(batch), err)
		}

		w.mutex.Lock()
		w.durableSequence = lastSequenceInBatch
		w.flushedBatchCount++
		if w.policy == FsyncEverySecond {
			w.hasUnsyncedWrites = true
		}
		w.batchDurable.Broadcast()
		w.mutex.Unlock()
	}
}

func (w *Writer) persistBatch(batch []byte) error {
	if _, err := w.file.Write(batch); err != nil {
		return fmt.Errorf("write aof file: %w", err)
	}
	if w.policy == FsyncAlways {
		if err := w.file.Sync(); err != nil {
			return fmt.Errorf("fsync aof file: %w", err)
		}
	}
	return nil
}

func (w *Writer) syncIfNeeded() error {
	w.mutex.Lock()
	needsSync := w.hasUnsyncedWrites
	w.hasUnsyncedWrites = false
	w.mutex.Unlock()

	if !needsSync {
		return nil
	}
	if err := w.file.Sync(); err != nil {
		w.mutex.Lock()
		w.hasUnsyncedWrites = true
		w.mutex.Unlock()
		return fmt.Errorf("fsync aof file: %w", err)
	}
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
			if err := w.syncIfNeeded(); err != nil {
				log.Printf("background aof fsync failed: %v", err)
			}
		}
	}
}
