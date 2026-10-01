package secrets

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/url"
	"sort"
	"strings"
)

// Marker replaces every secret match, whichever secret matched.
const Marker = "***REDACTED***"

const (
	minRedactLength = 8  // shorter values would redact ordinary output
	minPrefixLength = 12 // contract §12.3: any run of 12+ characters from the start
)

// Redactor removes credentials from bytes. It is applied to serialized output, not to data
// structures, so a credential nested anywhere in untyped upstream JSON is still caught.
type Redactor struct {
	replacer *strings.Replacer
}

// NewRedactor registers each secret in its raw, URL-encoded, base64, and JSON-escaped forms, every
// prefix of 12 or more characters, and any extra literal strings such as a full header value.
func NewRedactor(values []string, literals []string) *Redactor {
	patterns := map[string]bool{}
	for _, value := range values {
		if len(value) < minRedactLength {
			continue
		}
		forms := []string{
			value,
			url.QueryEscape(value),
			url.PathEscape(value),
			base64.StdEncoding.EncodeToString([]byte(value)),
			base64.RawStdEncoding.EncodeToString([]byte(value)),
			base64.URLEncoding.EncodeToString([]byte(value)),
			base64.RawURLEncoding.EncodeToString([]byte(value)),
			jsonEscaped(value),
		}
		for _, form := range forms {
			patterns[form] = true
		}
		for length := minPrefixLength; length < len(value); length++ {
			patterns[value[:length]] = true
		}
	}
	for _, literal := range literals {
		if len(literal) >= minRedactLength {
			patterns[literal] = true
			patterns[jsonEscaped(literal)] = true
		}
	}
	if len(patterns) == 0 {
		return &Redactor{}
	}
	ordered := make([]string, 0, len(patterns))
	for pattern := range patterns {
		ordered = append(ordered, pattern)
	}
	// Longest first, so a full match wins over any of its prefixes.
	sort.Slice(ordered, func(i, j int) bool {
		if len(ordered[i]) != len(ordered[j]) {
			return len(ordered[i]) > len(ordered[j])
		}
		return ordered[i] < ordered[j]
	})
	pairs := make([]string, 0, 2*len(ordered))
	for _, pattern := range ordered {
		pairs = append(pairs, pattern, Marker)
	}
	return &Redactor{replacer: strings.NewReplacer(pairs...)}
}

func (r *Redactor) Apply(content []byte) []byte {
	if r == nil || r.replacer == nil {
		return content
	}
	return []byte(r.replacer.Replace(string(content)))
}

func (r *Redactor) String(content string) string {
	if r == nil || r.replacer == nil {
		return content
	}
	return r.replacer.Replace(content)
}

// Writer wraps w so every write passes through the redactor. Callers write whole lines or whole
// documents, so a secret is never split across two writes.
func (r *Redactor) Writer(w io.Writer) io.Writer {
	return redactingWriter{redactor: r, target: w}
}

type redactingWriter struct {
	redactor *Redactor
	target   io.Writer
}

func (w redactingWriter) Write(p []byte) (int, error) {
	if _, err := w.target.Write(w.redactor.Apply(p)); err != nil {
		return 0, err
	}
	return len(p), nil
}

// jsonEscaped returns the value as it appears inside a JSON string written without HTML escaping,
// which is how the envelope is encoded.
func jsonEscaped(value string) string {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	encoded := bytes.TrimSpace(buffer.Bytes())
	return string(encoded[1 : len(encoded)-1])
}
