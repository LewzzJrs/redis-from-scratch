package resp

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func readerFor(input string) *Reader {
	return NewReader(strings.NewReader(input))
}

func TestReadValueParsesEachType(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected Value
	}{
		{"simple string", "+OK\r\n", NewSimpleString("OK")},
		{"error", "-ERR boom\r\n", NewError("ERR boom")},
		{"positive integer", ":1000\r\n", NewInteger(1000)},
		{"negative integer", ":-42\r\n", NewInteger(-42)},
		{"bulk string", "$5\r\nhello\r\n", NewBulkString("hello")},
		{"empty bulk string", "$0\r\n\r\n", NewBulkString("")},
		{"bulk string with CRLF inside", "$4\r\na\r\nb\r\n", NewBulkString("a\r\nb")},
		{"null bulk string", "$-1\r\n", NewNullBulkString()},
		{"empty array", "*0\r\n", NewArray()},
		{"null array", "*-1\r\n", NewNullArray()},
		{
			"array of mixed types",
			"*3\r\n$3\r\nfoo\r\n:7\r\n+bar\r\n",
			NewArray(NewBulkString("foo"), NewInteger(7), NewSimpleString("bar")),
		},
		{
			"nested array",
			"*2\r\n*1\r\n:1\r\n*1\r\n:2\r\n",
			NewArray(NewArray(NewInteger(1)), NewArray(NewInteger(2))),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			value, err := readerFor(testCase.input).ReadValue()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(value, testCase.expected) {
				t.Fatalf("got %#v, want %#v", value, testCase.expected)
			}
		})
	}
}

func TestReadCommand(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected []string
	}{
		{"resp array", "*3\r\n$3\r\nSET\r\n$3\r\nkey\r\n$5\r\nvalue\r\n", []string{"SET", "key", "value"}},
		{"inline with CRLF", "PING\r\n", []string{"PING"}},
		{"inline with LF only", "ECHO hi\n", []string{"ECHO", "hi"}},
		{"inline with extra spaces", "  SET   a   b  \r\n", []string{"SET", "a", "b"}},
		{"empty inline line", "\r\n", []string{}},
		{"empty array", "*0\r\n", []string{}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			args, err := readerFor(testCase.input).ReadCommand()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(args, testCase.expected) {
				t.Fatalf("got %q, want %q", args, testCase.expected)
			}
		})
	}
}

func TestReadCommandHandlesPipelinedCommands(t *testing.T) {
	reader := readerFor("*1\r\n$4\r\nPING\r\nECHO hi\r\n*2\r\n$3\r\nGET\r\n$1\r\nk\r\n")
	expectedCommands := [][]string{{"PING"}, {"ECHO", "hi"}, {"GET", "k"}}

	for _, expected := range expectedCommands {
		args, err := reader.ReadCommand()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(args, expected) {
			t.Fatalf("got %q, want %q", args, expected)
		}
	}

	if _, err := reader.ReadCommand(); !errors.Is(err, io.EOF) {
		t.Fatalf("got %v after last command, want io.EOF", err)
	}
}

func expectProtocolError(t *testing.T, err error) {
	t.Helper()
	var protocolError *ProtocolError
	if !errors.As(err, &protocolError) {
		t.Fatalf("got %v, want *ProtocolError", err)
	}
}

func TestReadValueRejectsMalformedInput(t *testing.T) {
	testCases := []struct {
		name  string
		input string
	}{
		{"unknown type byte", "?abc\r\n"},
		{"non numeric bulk length", "$abc\r\n"},
		{"bulk length below -1", "$-2\r\n"},
		{"bulk length above limit", "$600000000\r\n"},
		{"bulk without CRLF terminator", "$3\r\nabcXY"},
		{"non numeric array length", "*x\r\n"},
		{"array length below -1", "*-5\r\n"},
		{"non numeric integer", ":12a\r\n"},
		{"empty line", "*1\r\n\r\n"},
		{"nesting too deep", strings.Repeat("*1\r\n", maxNestingDepth+5) + ":1\r\n"},
		{"line too long", "+" + strings.Repeat("a", readBufferSize+10) + "\r\n"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := readerFor(testCase.input).ReadValue()
			expectProtocolError(t, err)
		})
	}
}

func TestReadCommandRejectsInvalidCommands(t *testing.T) {
	testCases := []struct {
		name  string
		input string
	}{
		{"malformed bulk inside command", "*1\r\n$abc\r\n"},
		{"integer argument", "*1\r\n:1\r\n"},
		{"null argument", "*1\r\n$-1\r\n"},
		{"nested array argument", "*1\r\n*0\r\n"},
		{"inline line too long", strings.Repeat("a", readBufferSize+10) + "\r\n"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := readerFor(testCase.input).ReadCommand()
			expectProtocolError(t, err)
		})
	}
}

func TestReadReportsTruncatedInput(t *testing.T) {
	truncatedInputs := []string{
		"+OK",
		"$5\r\nab",
		"*2\r\n$1\r\na\r\n",
		"*1\r\n",
	}

	for _, input := range truncatedInputs {
		_, err := readerFor(input).ReadValue()
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("input %q: got %v, want io.ErrUnexpectedEOF", input, err)
		}
	}
}

func TestReadReportsCleanEOF(t *testing.T) {
	if _, err := readerFor("").ReadCommand(); !errors.Is(err, io.EOF) {
		t.Fatalf("got %v, want io.EOF", err)
	}
}

func encode(t *testing.T, value Value) string {
	t.Helper()

	var output bytes.Buffer
	writer := NewWriter(&output)
	if err := writer.Write(value); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	return output.String()
}

func TestWriterEncodesEachType(t *testing.T) {
	testCases := []struct {
		name     string
		value    Value
		expected string
	}{
		{"simple string", NewSimpleString("OK"), "+OK\r\n"},
		{"error", NewError("ERR boom"), "-ERR boom\r\n"},
		{"error with line breaks", NewError("ERR a\r\nb"), "-ERR a  b\r\n"},
		{"integer", NewInteger(-12), ":-12\r\n"},
		{"bulk string", NewBulkString("hello"), "$5\r\nhello\r\n"},
		{"empty bulk string", NewBulkString(""), "$0\r\n\r\n"},
		{"null bulk string", NewNullBulkString(), "$-1\r\n"},
		{"empty array", NewArray(), "*0\r\n"},
		{"null array", NewNullArray(), "*-1\r\n"},
		{"command", NewCommand("SET", "k", "v"), "*3\r\n$3\r\nSET\r\n$1\r\nk\r\n$1\r\nv\r\n"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if encoded := encode(t, testCase.value); encoded != testCase.expected {
				t.Fatalf("got %q, want %q", encoded, testCase.expected)
			}
		})
	}
}

func TestWriterRejectsUnknownType(t *testing.T) {
	writer := NewWriter(io.Discard)
	if err := writer.Write(Value{Type: '!'}); err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestWriteThenReadRoundTrip(t *testing.T) {
	values := []Value{
		NewSimpleString("PONG"),
		NewError("ERR something"),
		NewInteger(9223372036854775807),
		NewBulkString("binary\r\n\x00data"),
		NewNullBulkString(),
		NewNullArray(),
		NewArray(NewBulkString("a"), NewArray(NewInteger(1), NewNullBulkString()), NewArray()),
	}

	for _, original := range values {
		decoded, err := readerFor(encode(t, original)).ReadValue()
		if err != nil {
			t.Fatalf("read back %#v: %v", original, err)
		}
		if !reflect.DeepEqual(decoded, original) {
			t.Fatalf("round trip changed value: got %#v, want %#v", decoded, original)
		}
	}
}
