package shape

import (
	"regexp"
	"strings"

	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
)

var pathPattern = regexp.MustCompile(`^[A-Za-z0-9_]+(\.[A-Za-z0-9_]+)*(\.\*)?$`)

// ResolveFields turns a --fields expression into the ordered list of paths to project
// (contract §8.2). An empty expression selects the defaults. available may contain "prefix.*"
// entries, which admit every path under prefix.
func ResolveFields(expression string, defaults, available []string) ([]string, *errs.Error) {
	if strings.TrimSpace(expression) == "" {
		return append([]string(nil), defaults...), nil
	}
	items := strings.Split(expression, ",")
	var additive, subtractive []string
	for _, raw := range items {
		item := strings.TrimSpace(raw)
		switch {
		case item == "*":
			additive = append(additive, available...)
		case strings.HasPrefix(item, "-"):
			path := item[1:]
			if err := checkPath(path, available); err != nil {
				return nil, err
			}
			subtractive = append(subtractive, path)
		default:
			if err := checkPath(item, available); err != nil {
				return nil, err
			}
			additive = append(additive, item)
		}
	}
	if len(additive) > 0 && len(subtractive) > 0 {
		return nil, errs.Usagef("--fields cannot add and remove fields in one expression.").
			WithHint("Use either --fields a,b,c or --fields -a,-b.").
			WithDetail("fields", expression)
	}
	if len(subtractive) > 0 {
		removed := map[string]bool{}
		for _, path := range subtractive {
			removed[path] = true
		}
		var kept []string
		for _, path := range defaults {
			if !removed[path] {
				kept = append(kept, path)
			}
		}
		return kept, nil
	}
	return dedupe(additive), nil
}

func checkPath(path string, available []string) *errs.Error {
	if !pathPattern.MatchString(path) {
		return errs.Usagef("--fields item %q is not a field path; use names separated by dots, optionally ending in .*.", path).
			WithDetail("available_fields", available)
	}
	if isAvailable(path, available) {
		return nil
	}
	err := errs.Usagef("Field %q is not available for this command.", path).
		WithDetail("field", path).WithDetail("available_fields", available)
	if suggestion := Closest(path, available); suggestion != "" {
		err.WithDetail("did_you_mean", suggestion)
	}
	return err
}

func isAvailable(path string, available []string) bool {
	for _, entry := range available {
		if entry == path {
			return true
		}
		if prefix, ok := strings.CutSuffix(entry, ".*"); ok {
			if path == prefix || strings.HasPrefix(path, prefix+".") {
				return true
			}
		}
	}
	return false
}

func dedupe(paths []string) []string {
	seen := map[string]bool{}
	var unique []string
	for _, path := range paths {
		if !seen[path] {
			seen[path] = true
			unique = append(unique, path)
		}
	}
	return unique
}

// Project keeps only the given paths of an object, nested as in the source, in the requested
// order. A path missing from the record is omitted (contract §8.2 rule 4).
func Project(record Value, paths []string) Value {
	out := NewObject()
	for _, path := range paths {
		segments := strings.Split(strings.TrimSuffix(path, ".*"), ".")
		value, ok := record.Lookup(segments)
		if !ok {
			continue
		}
		out = insert(out, segments, value)
	}
	return out
}

func insert(object Value, segments []string, value Value) Value {
	key := segments[0]
	for index, field := range object.Fields {
		if field.Key != key {
			continue
		}
		if len(segments) == 1 {
			return object
		}
		if field.Value.Kind == Object {
			object.Fields[index].Value = insert(field.Value, segments[1:], value)
		}
		return object
	}
	if len(segments) == 1 {
		object.Fields = append(object.Fields, Field{Key: key, Value: value})
		return object
	}
	object.Fields = append(object.Fields, Field{Key: key, Value: insert(NewObject(), segments[1:], value)})
	return object
}

// Closest returns the candidate nearest to input by edit distance, or "" when none is close.
func Closest(input string, candidates []string) string {
	best, bestDistance := "", -1
	for _, candidate := range candidates {
		distance := levenshtein(strings.ToLower(input), strings.ToLower(candidate))
		if bestDistance < 0 || distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	limit := len(input) / 3
	if limit < 2 {
		limit = 2
	}
	if bestDistance < 0 || bestDistance > limit {
		return ""
	}
	return best
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	previous := make([]int, len(rb)+1)
	current := make([]int, len(rb)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		current[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(rb)]
}
