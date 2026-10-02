package envelope

import (
	"github.com/guyravid/ai/cli/tools/trello/internal/shape"
)

const minShortenedString = 16

// CursorAt returns the cursor that resumes at the record with the given index in this response.
type CursorAt func(index int) string

// FitList sets data to the records and, if the encoded envelope exceeds maxBytes, drops whole
// records from the end until it fits (contract §8.4). meta.page must already describe the
// untruncated response. It returns the encoded bytes.
func FitList(e *Envelope, records []shape.Value, maxBytes int, pretty bool, cursorAt CursorAt) []byte {
	original := *e.Meta.Page
	build := func(kept int) []byte {
		page := original
		page.Count = kept
		if kept < len(records) {
			cursor := cursorAt(kept)
			page.HasMore = true
			page.NextCursor = &cursor
			page.Truncated = true
			page.TruncatedReason = "max_bytes"
			page.Dropped = len(records) - kept
		}
		e.Meta.Page = &page
		e.Data = shape.NewArray(records[:kept]...)
		return Encode(e, pretty)
	}

	full := build(len(records))
	if len(full) <= maxBytes {
		return full
	}
	low, high := 0, len(records)-1 // the largest count that fits lies in [low, high]
	for low < high {
		middle := (low + high + 1) / 2
		if len(build(middle)) <= maxBytes {
			low = middle
		} else {
			high = middle - 1
		}
	}
	if low > 0 {
		return build(low)
	}
	if len(records) == 0 {
		build(0)
		return belowMinimum(e, pretty)
	}
	// Not even one record fits. Return the first one shortened, so the caller always makes
	// progress, and still report that the rest was dropped.
	baseElided := e.Meta.ElidedFields
	first, elided := shrinkToFit(records[0], func(candidate shape.Value, elided []string) int {
		records[0] = candidate
		e.Meta.ElidedFields = append(append([]string(nil), baseElided...), prefixPaths("[0]", elided)...)
		return len(build(1))
	}, maxBytes)
	records[0] = first
	e.Meta.ElidedFields = append(append([]string(nil), baseElided...), prefixPaths("[0]", elided)...)
	encoded := build(1)
	if len(encoded) > maxBytes {
		return belowMinimum(e, pretty)
	}
	return encoded
}

// belowMinimum reports that the cap could not be met even with the smallest useful response; the
// cap is then ignored for the envelope.
func belowMinimum(e *Envelope, pretty bool) []byte {
	e.Meta.Page.Truncated = true
	e.Meta.Page.TruncatedReason = "max_bytes_below_minimum"
	if e.Meta.Page.NextCursor == nil {
		e.Meta.Page.HasMore = false
	}
	return Encode(e, pretty)
}

// FitObject shortens the longest strings of a single-object response until it fits. data never
// becomes null because of size.
func FitObject(e *Envelope, object shape.Value, maxBytes int, pretty bool) []byte {
	e.Data = object
	encoded := Encode(e, pretty)
	if len(encoded) <= maxBytes {
		return encoded
	}
	baseElided := e.Meta.ElidedFields
	shrunk, elided := shrinkToFit(object, func(candidate shape.Value, elided []string) int {
		e.Data = candidate
		e.Meta.ElidedFields = append(append([]string(nil), baseElided...), elided...)
		return len(Encode(e, pretty))
	}, maxBytes)
	e.Data = shrunk
	e.Meta.ElidedFields = append(append([]string(nil), baseElided...), elided...)
	return Encode(e, pretty)
}

// shrinkToFit halves the longest string until measure, which sees the elided paths so far, fits.
// Each string is always shortened from its original text, so the marker's count stays true and
// markers never stack. Every step strictly lowers how much of one string is kept, down to
// minShortenedString, so the loop ends even when nothing more can be saved.
func shrinkToFit(value shape.Value, measure func(shape.Value, []string) int, maxBytes int) (shape.Value, []string) {
	var elided []string
	originals := map[string]string{} // path -> text before any shortening
	kept := map[string]int{}         // path -> characters of the original kept so far
	for measure(value, elided) > maxBytes {
		shortened := false
		for _, ref := range shape.LongestStrings(value) {
			current, wasShortened := kept[ref.Path]
			if !wasShortened {
				current = ref.Length
			}
			keep := max(current/2, minShortenedString)
			if keep >= current {
				continue
			}
			if !wasShortened {
				originals[ref.Path] = shape.TextAt(value, ref)
				elided = append(elided, ref.Path)
			}
			kept[ref.Path] = keep
			value = shape.ReplaceString(value, ref, shape.Shorten(originals[ref.Path], keep))
			shortened = true
			break
		}
		if !shortened {
			break
		}
	}
	return value, elided
}

func prefixPaths(prefix string, paths []string) []string {
	prefixed := make([]string, len(paths))
	for index, path := range paths {
		if path == "." {
			prefixed[index] = prefix
		} else {
			prefixed[index] = prefix + "." + path
		}
	}
	return prefixed
}
