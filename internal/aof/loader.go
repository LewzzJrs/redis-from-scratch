package aof

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/LewzzJrs/redis-from-scratch/internal/resp"
)

var ErrCorrupted = errors.New("append-only file is corrupted")

type CommandExecutor interface {
	Execute(args []string) resp.Value
}

type LoadResult struct {
	CommandsReplayed int
	TruncatedBytes   int64
}

type countingReader struct {
	source    io.Reader
	bytesRead int64
}

func (c *countingReader) Read(buffer []byte) (int, error) {
	readCount, err := c.source.Read(buffer)
	c.bytesRead += int64(readCount)
	return readCount, err
}

func Load(path string, executor CommandExecutor) (LoadResult, error) {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return LoadResult{}, nil
	}
	if err != nil {
		return LoadResult{}, fmt.Errorf("open aof file: %w", err)
	}
	defer file.Close()

	result, validLength, readError := replayCommands(file, executor)
	if readError == nil {
		return result, nil
	}
	if !errors.Is(readError, io.ErrUnexpectedEOF) {
		return result, readError
	}

	fileInfo, err := file.Stat()
	if err != nil {
		return result, fmt.Errorf("inspect aof file: %w", err)
	}
	file.Close()

	if err := os.Truncate(path, validLength); err != nil {
		return result, fmt.Errorf("truncate incomplete command at byte %d: %w", validLength, err)
	}
	result.TruncatedBytes = fileInfo.Size() - validLength
	return result, nil
}

func replayCommands(source io.Reader, executor CommandExecutor) (LoadResult, int64, error) {
	byteCounter := &countingReader{source: source}
	reader := resp.NewReader(byteCounter)

	var result LoadResult
	validLength := int64(0)

	for {
		args, err := reader.ReadCommand()
		if errors.Is(err, io.EOF) {
			return result, validLength, nil
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return result, validLength, err
		}
		if err != nil {
			return result, validLength, fmt.Errorf("%w at byte %d: %v", ErrCorrupted, validLength, err)
		}

		if len(args) > 0 {
			reply := executor.Execute(args)
			if reply.Type == resp.Error {
				return result, validLength, fmt.Errorf("%w at byte %d: replaying %s failed: %s", ErrCorrupted, validLength, args[0], reply.Text)
			}
			result.CommandsReplayed++
		}

		validLength = byteCounter.bytesRead - int64(reader.Buffered())
	}
}
