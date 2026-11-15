package resp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	readBufferSize   = 64 * 1024
	maxBulkLength    = 512 * 1024 * 1024
	maxArrayLength   = 1024 * 1024
	maxNestingDepth  = 32
	preallocateLimit = 1024
)

type ProtocolError struct {
	Reason string
}

func (e *ProtocolError) Error() string {
	return "protocol error: " + e.Reason
}

func newProtocolError(format string, args ...any) error {
	return &ProtocolError{Reason: fmt.Sprintf(format, args...)}
}

type Reader struct {
	reader *bufio.Reader
}

func NewReader(source io.Reader) *Reader {
	return &Reader{reader: bufio.NewReaderSize(source, readBufferSize)}
}

func (r *Reader) Buffered() int {
	return r.reader.Buffered()
}

func (r *Reader) ReadValue() (Value, error) {
	return r.readValue(0)
}

func (r *Reader) ReadCommand() ([]string, error) {
	firstByte, err := r.reader.Peek(1)
	if err != nil {
		return nil, err
	}
	if Type(firstByte[0]) != Array {
		return r.readInlineCommand()
	}

	value, err := r.ReadValue()
	if err != nil {
		return nil, err
	}
	if value.IsNull {
		return []string{}, nil
	}

	args := make([]string, len(value.Items))
	for index, item := range value.Items {
		if item.Type != BulkString || item.IsNull {
			return nil, newProtocolError("expected bulk string as command argument")
		}
		args[index] = item.Text
	}
	return args, nil
}

func (r *Reader) readInlineCommand() ([]string, error) {
	line, err := r.readLine()
	if err != nil {
		return nil, err
	}
	return strings.Fields(line), nil
}

func (r *Reader) readValue(depth int) (Value, error) {
	if depth > maxNestingDepth {
		return Value{}, newProtocolError("nesting deeper than %d levels", maxNestingDepth)
	}

	line, err := r.readLine()
	if err != nil {
		return Value{}, err
	}
	if line == "" {
		return Value{}, newProtocolError("empty line where a value was expected")
	}

	valueType, payload := Type(line[0]), line[1:]
	switch valueType {
	case SimpleString:
		return NewSimpleString(payload), nil
	case Error:
		return NewError(payload), nil
	case Integer:
		number, err := strconv.ParseInt(payload, 10, 64)
		if err != nil {
			return Value{}, newProtocolError("invalid integer %q", payload)
		}
		return NewInteger(number), nil
	case BulkString:
		return r.readBulkString(payload)
	case Array:
		return r.readArray(payload, depth)
	default:
		return Value{}, newProtocolError("unexpected type byte %q", line[0])
	}
}

func (r *Reader) readBulkString(lengthText string) (Value, error) {
	length, err := strconv.Atoi(lengthText)
	if err != nil || length < -1 || length > maxBulkLength {
		return Value{}, newProtocolError("invalid bulk length %q", lengthText)
	}
	if length == -1 {
		return NewNullBulkString(), nil
	}

	payloadWithTerminator := make([]byte, length+2)
	if _, err := io.ReadFull(r.reader, payloadWithTerminator); err != nil {
		return Value{}, unexpectedEOF(err)
	}
	if payloadWithTerminator[length] != '\r' || payloadWithTerminator[length+1] != '\n' {
		return Value{}, newProtocolError("bulk string not terminated by CRLF")
	}
	return NewBulkString(string(payloadWithTerminator[:length])), nil
}

func (r *Reader) readArray(countText string, depth int) (Value, error) {
	count, err := strconv.Atoi(countText)
	if err != nil || count < -1 || count > maxArrayLength {
		return Value{}, newProtocolError("invalid multibulk length %q", countText)
	}
	if count == -1 {
		return NewNullArray(), nil
	}

	items := make([]Value, 0, min(count, preallocateLimit))
	for range count {
		item, err := r.readValue(depth + 1)
		if err != nil {
			return Value{}, unexpectedEOF(err)
		}
		items = append(items, item)
	}
	return NewArray(items...), nil
}

func (r *Reader) readLine() (string, error) {
	line, err := r.reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		return "", newProtocolError("line longer than %d bytes", readBufferSize)
	}
	if err != nil {
		if errors.Is(err, io.EOF) && len(line) > 0 {
			return "", io.ErrUnexpectedEOF
		}
		return "", err
	}

	line = line[:len(line)-1]
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return string(line), nil
}

func unexpectedEOF(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}
