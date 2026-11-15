package resp

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const crlf = "\r\n"

var lineBreakRemover = strings.NewReplacer("\r", " ", "\n", " ")

type Writer struct {
	writer        *bufio.Writer
	numberScratch []byte
}

func NewWriter(destination io.Writer) *Writer {
	return &Writer{writer: bufio.NewWriter(destination)}
}

func (w *Writer) Write(value Value) error {
	switch value.Type {
	case SimpleString, Error:
		w.writer.WriteByte(byte(value.Type))
		w.writer.WriteString(lineBreakRemover.Replace(value.Text))
		_, err := w.writer.WriteString(crlf)
		return err
	case Integer:
		return w.writeHeader(Integer, value.Number)
	case BulkString:
		if value.IsNull {
			return w.writeHeader(BulkString, -1)
		}
		w.writeHeader(BulkString, int64(len(value.Text)))
		w.writer.WriteString(value.Text)
		_, err := w.writer.WriteString(crlf)
		return err
	case Array:
		if value.IsNull {
			return w.writeHeader(Array, -1)
		}
		if err := w.writeHeader(Array, int64(len(value.Items))); err != nil {
			return err
		}
		for _, item := range value.Items {
			if err := w.Write(item); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("cannot encode value with unknown type %q", value.Type)
	}
}

func (w *Writer) Flush() error {
	return w.writer.Flush()
}

func (w *Writer) writeHeader(valueType Type, number int64) error {
	w.writer.WriteByte(byte(valueType))
	w.numberScratch = strconv.AppendInt(w.numberScratch[:0], number, 10)
	w.writer.Write(w.numberScratch)
	_, err := w.writer.WriteString(crlf)
	return err
}
