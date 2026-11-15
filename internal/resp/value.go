package resp

type Type byte

const (
	SimpleString Type = '+'
	Error        Type = '-'
	Integer      Type = ':'
	BulkString   Type = '$'
	Array        Type = '*'
)

type Value struct {
	Type   Type
	Text   string
	Number int64
	Items  []Value
	IsNull bool
}

func NewSimpleString(text string) Value {
	return Value{Type: SimpleString, Text: text}
}

func NewError(message string) Value {
	return Value{Type: Error, Text: message}
}

func NewInteger(number int64) Value {
	return Value{Type: Integer, Number: number}
}

func NewBulkString(text string) Value {
	return Value{Type: BulkString, Text: text}
}

func NewNullBulkString() Value {
	return Value{Type: BulkString, IsNull: true}
}

func NewArray(items ...Value) Value {
	if items == nil {
		items = []Value{}
	}
	return Value{Type: Array, Items: items}
}

func NewNullArray() Value {
	return Value{Type: Array, IsNull: true}
}

func NewCommand(args ...string) Value {
	items := make([]Value, len(args))
	for index, arg := range args {
		items[index] = NewBulkString(arg)
	}
	return NewArray(items...)
}
