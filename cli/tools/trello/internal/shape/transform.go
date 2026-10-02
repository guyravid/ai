package shape

import (
	"bytes"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"unicode/utf8"
)

// IsEmpty reports whether a value is null, "", [], or {} (contract §8.3). false and 0 are values.
func (v Value) IsEmpty() bool {
	switch v.Kind {
	case Object:
		return len(v.Fields) == 0
	case Array:
		return len(v.Items) == 0
	default:
		return v.IsNull() || bytes.Equal(v.Raw, []byte(`""`))
	}
}

// StripEmpty removes empty object members recursively, bottom-up. Array elements are kept, since
// removing one would change the array's meaning, but objects inside arrays are stripped.
func StripEmpty(v Value) Value {
	switch v.Kind {
	case Object:
		kept := make([]Field, 0, len(v.Fields))
		for _, field := range v.Fields {
			stripped := StripEmpty(field.Value)
			if !stripped.IsEmpty() {
				kept = append(kept, Field{Key: field.Key, Value: stripped})
			}
		}
		return Value{Kind: Object, Fields: kept}
	case Array:
		items := make([]Value, len(v.Items))
		for index, item := range v.Items {
			items[index] = StripEmpty(item)
		}
		return Value{Kind: Array, Items: items}
	default:
		return v
	}
}

// DepthElided replaces a subtree deeper than the cap.
var DepthElided = String("<depth-elided>")

// Cap applies --max-string and --max-depth (contract §8.4), returning the paths it shortened.
// depth counts nesting below the value passed in: its direct members are at depth 1.
func Cap(v Value, maxString, maxDepth int, path string) (Value, []string) {
	var elided []string
	capped := capValue(v, maxString, maxDepth, 0, path, &elided)
	return capped, elided
}

func capValue(v Value, maxString, maxDepth, depth int, path string, elided *[]string) Value {
	switch v.Kind {
	case Object:
		if depth >= maxDepth && len(v.Fields) > 0 {
			*elided = append(*elided, displayPath(path))
			return DepthElided
		}
		fields := make([]Field, len(v.Fields))
		for index, field := range v.Fields {
			fields[index] = Field{Key: field.Key, Value: capValue(field.Value, maxString, maxDepth, depth+1, joinPath(path, field.Key), elided)}
		}
		return Value{Kind: Object, Fields: fields}
	case Array:
		if depth >= maxDepth && len(v.Items) > 0 {
			*elided = append(*elided, displayPath(path))
			return DepthElided
		}
		items := make([]Value, len(v.Items))
		for index, item := range v.Items {
			items[index] = capValue(item, maxString, maxDepth, depth+1, fmt.Sprintf("%s[%d]", path, index), elided)
		}
		return Value{Kind: Array, Items: items}
	default:
		if v.IsString() {
			text := v.Text()
			if count := utf8.RuneCountInString(text); count > maxString {
				*elided = append(*elided, displayPath(path))
				return String(Shorten(text, maxString))
			}
		}
		return v
	}
}

// Shorten keeps the first keep characters and appends the elision marker.
func Shorten(text string, keep int) string {
	runes := []rune(text)
	if len(runes) <= keep {
		return text
	}
	return string(runes[:keep]) + fmt.Sprintf("…[+%d chars]", len(runes)-keep)
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func displayPath(path string) string {
	if path == "" {
		return "."
	}
	return path
}

// LongestStrings returns the paths of string scalars ordered longest first, for shortening a single
// object that exceeds the byte cap.
func LongestStrings(v Value) []StringRef {
	var refs []StringRef
	collectStrings(v, "", nil, &refs)
	sort.SliceStable(refs, func(i, j int) bool { return refs[i].Length > refs[j].Length })
	return refs
}

type StringRef struct {
	Path   string
	Length int
	steps  []step
}

type step struct {
	key   string
	index int
	isKey bool
}

func collectStrings(v Value, path string, steps []step, refs *[]StringRef) {
	switch v.Kind {
	case Object:
		for _, field := range v.Fields {
			next := append(append([]step(nil), steps...), step{key: field.Key, isKey: true})
			collectStrings(field.Value, joinPath(path, field.Key), next, refs)
		}
	case Array:
		for index, item := range v.Items {
			next := append(append([]step(nil), steps...), step{index: index})
			collectStrings(item, fmt.Sprintf("%s[%d]", path, index), next, refs)
		}
	default:
		if v.IsString() {
			*refs = append(*refs, StringRef{Path: displayPath(path), Length: utf8.RuneCountInString(v.Text()), steps: steps})
		}
	}
}

// ReplaceString puts text at ref.
func ReplaceString(v Value, ref StringRef, text string) Value {
	return replaceAt(v, ref.steps, func(Value) Value { return String(text) })
}

// TextAt returns the string at ref.
func TextAt(v Value, ref StringRef) string {
	var text string
	replaceAt(v, ref.steps, func(old Value) Value {
		text = old.Text()
		return old
	})
	return text
}

func replaceAt(v Value, steps []step, change func(Value) Value) Value {
	if len(steps) == 0 {
		return change(v)
	}
	current := steps[0]
	switch v.Kind {
	case Object:
		fields := append([]Field(nil), v.Fields...)
		for index, field := range fields {
			if current.isKey && field.Key == current.key {
				fields[index].Value = replaceAt(field.Value, steps[1:], change)
			}
		}
		return Value{Kind: Object, Fields: fields}
	case Array:
		items := append([]Value(nil), v.Items...)
		if !current.isKey && current.index < len(items) {
			items[current.index] = replaceAt(items[current.index], steps[1:], change)
		}
		return Value{Kind: Array, Items: items}
	}
	return v
}

// SortKey is one term of a declared sort, such as "dateLastActivity desc".
type SortKey struct {
	Path       []string
	Descending bool
}

// ParseSort parses "dateLastActivity desc, id asc".
func ParseSort(declared string) []SortKey {
	var keys []SortKey
	for _, term := range strings.Split(declared, ",") {
		parts := strings.Fields(term)
		if len(parts) == 0 {
			continue
		}
		keys = append(keys, SortKey{
			Path:       strings.Split(parts[0], "."),
			Descending: len(parts) > 1 && parts[1] == "desc",
		})
	}
	return keys
}

// SortRecords orders records by the declared keys. Records missing a key sort after those that
// have it, in either direction, so the order is total whenever the last key is unique.
func SortRecords(records []Value, keys []SortKey) {
	sort.SliceStable(records, func(i, j int) bool {
		for _, key := range keys {
			left, leftOK := records[i].Lookup(key.Path)
			right, rightOK := records[j].Lookup(key.Path)
			leftOK = leftOK && !left.IsNull()
			rightOK = rightOK && !right.IsNull()
			if leftOK != rightOK {
				return leftOK
			}
			if !leftOK {
				continue
			}
			comparison := compareScalars(left, right)
			if comparison == 0 {
				continue
			}
			if key.Descending {
				return comparison > 0
			}
			return comparison < 0
		}
		return false
	})
}

func compareScalars(left, right Value) int {
	if left.IsNumber() && right.IsNumber() {
		leftNumber, leftParsed := new(big.Float).SetPrec(128).SetString(string(left.Raw))
		rightNumber, rightParsed := new(big.Float).SetPrec(128).SetString(string(right.Raw))
		if leftParsed && rightParsed {
			return leftNumber.Cmp(rightNumber)
		}
	}
	if left.IsString() && right.IsString() {
		return strings.Compare(left.Text(), right.Text())
	}
	return strings.Compare(string(left.Marshal()), string(right.Marshal()))
}
