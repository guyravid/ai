// Package shape holds the ordered JSON value used for all output, and the projection, stripping,
// capping, and ordering applied to it (contract §8, §14).
package shape

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"unicode/utf8"
)

type Kind int

const (
	Scalar Kind = iota
	Object
	Array
)

// Value is a JSON value that keeps object member order and the exact text of numbers.
type Value struct {
	Kind   Kind
	Raw    []byte // scalars only: the JSON text of a string, number, true, false, or null
	Fields []Field
	Items  []Value
}

type Field struct {
	Key   string
	Value Value
}

var nullRaw = []byte("null")

func Null() Value                     { return Value{Kind: Scalar, Raw: nullRaw} }
func NewObject(fields ...Field) Value { return Value{Kind: Object, Fields: fields} }
func NewArray(items ...Value) Value   { return Value{Kind: Array, Items: items} }

func String(text string) Value { return Value{Kind: Scalar, Raw: encodeString(text)} }
func Int(number int64) Value   { return Value{Kind: Scalar, Raw: []byte(strconv.FormatInt(number, 10))} }
func Bool(flag bool) Value {
	if flag {
		return Value{Kind: Scalar, Raw: []byte("true")}
	}
	return Value{Kind: Scalar, Raw: []byte("false")}
}

// FromAny encodes a Go value through encoding/json, then parses it into an ordered Value. Map keys
// come out sorted, which is stable.
func FromAny(value any) (Value, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return Value{}, err
	}
	return Parse(buffer.Bytes())
}

func (v Value) IsNull() bool   { return v.Kind == Scalar && bytes.Equal(v.Raw, nullRaw) }
func (v Value) IsString() bool { return v.Kind == Scalar && len(v.Raw) > 0 && v.Raw[0] == '"' }
func (v Value) IsNumber() bool {
	return v.Kind == Scalar && len(v.Raw) > 0 && (v.Raw[0] == '-' || (v.Raw[0] >= '0' && v.Raw[0] <= '9'))
}

// Text returns the decoded string of a string scalar.
func (v Value) Text() string {
	var text string
	_ = json.Unmarshal(v.Raw, &text)
	return text
}

// Get returns the member with the given key of an object.
func (v Value) Get(key string) (Value, bool) {
	if v.Kind != Object {
		return Value{}, false
	}
	for _, field := range v.Fields {
		if field.Key == key {
			return field.Value, true
		}
	}
	return Value{}, false
}

// Lookup follows a dotted path through nested objects.
func (v Value) Lookup(path []string) (Value, bool) {
	current := v
	for _, key := range path {
		next, ok := current.Get(key)
		if !ok {
			return Value{}, false
		}
		current = next
	}
	return current, true
}

// Parse decodes JSON into an ordered Value, keeping numbers as written.
func Parse(content []byte) (Value, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	value, err := parseValue(decoder)
	if err != nil {
		return Value{}, err
	}
	if decoder.More() {
		return Value{}, fmt.Errorf("unexpected content after the JSON value")
	}
	return value, nil
}

func parseValue(decoder *json.Decoder) (Value, error) {
	token, err := decoder.Token()
	if err != nil {
		return Value{}, err
	}
	switch typed := token.(type) {
	case json.Delim:
		switch typed {
		case '{':
			object := Value{Kind: Object, Fields: []Field{}}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return Value{}, err
				}
				key, _ := keyToken.(string)
				member, err := parseValue(decoder)
				if err != nil {
					return Value{}, err
				}
				object.Fields = append(object.Fields, Field{Key: key, Value: member})
			}
			if _, err := decoder.Token(); err != nil {
				return Value{}, err
			}
			return object, nil
		case '[':
			array := Value{Kind: Array, Items: []Value{}}
			for decoder.More() {
				item, err := parseValue(decoder)
				if err != nil {
					return Value{}, err
				}
				array.Items = append(array.Items, item)
			}
			if _, err := decoder.Token(); err != nil {
				return Value{}, err
			}
			return array, nil
		}
		return Value{}, fmt.Errorf("unexpected delimiter %v", typed)
	case string:
		return String(typed), nil
	case json.Number:
		return Value{Kind: Scalar, Raw: []byte(typed.String())}, nil
	case bool:
		return Bool(typed), nil
	case nil:
		return Null(), nil
	}
	return Value{}, fmt.Errorf("unexpected token %v", token)
}

// Marshal returns compact JSON for the value.
func (v Value) Marshal() []byte { return v.AppendJSON(nil) }

// MarshalJSON lets a Value sit inside structs encoded by encoding/json.
func (v Value) MarshalJSON() ([]byte, error) {
	if v.Kind == Scalar && v.Raw == nil {
		return nullRaw, nil
	}
	return v.Marshal(), nil
}

func (v Value) AppendJSON(buffer []byte) []byte {
	switch v.Kind {
	case Object:
		buffer = append(buffer, '{')
		for index, field := range v.Fields {
			if index > 0 {
				buffer = append(buffer, ',')
			}
			buffer = append(buffer, encodeString(field.Key)...)
			buffer = append(buffer, ':')
			buffer = field.Value.AppendJSON(buffer)
		}
		return append(buffer, '}')
	case Array:
		buffer = append(buffer, '[')
		for index, item := range v.Items {
			if index > 0 {
				buffer = append(buffer, ',')
			}
			buffer = item.AppendJSON(buffer)
		}
		return append(buffer, ']')
	default:
		if v.Raw == nil {
			return append(buffer, nullRaw...)
		}
		return append(buffer, v.Raw...)
	}
}

func encodeString(text string) []byte {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if !utf8.ValidString(text) {
		text = string([]rune(text))
	}
	_ = encoder.Encode(text)
	return bytes.TrimRight(buffer.Bytes(), "\n")
}
