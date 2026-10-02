// Package envelope defines the response document (contract §3) and encodes it. Nothing else in the
// tool builds the top-level JSON.
package envelope

import (
	"bytes"
	"encoding/json"
	"sort"
	"unicode/utf8"

	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
	"github.com/guyravid/ai/cli/tools/trello/internal/shape"
)

const ContractVersion = "1.4"

const (
	maxMessageChars = 512
	maxDetailsBytes = 2048
)

type Envelope struct {
	OK      bool        `json:"ok"`
	Tool    string      `json:"tool"`
	Command *string     `json:"command"`
	Data    shape.Value `json:"data"`
	Error   *ErrorBody  `json:"error,omitempty"`
	Meta    Meta        `json:"meta"`
}

type ErrorBody struct {
	Code         errs.Code      `json:"code"`
	ExitCode     int            `json:"exit_code"`
	Message      string         `json:"message"`
	Retriable    bool           `json:"retriable"`
	RetryAfterMs *int64         `json:"retry_after_ms,omitempty"`
	Hint         string         `json:"hint,omitempty"`
	Details      map[string]any `json:"details,omitempty"`
}

type Meta struct {
	Page            *Page            `json:"page,omitempty"`
	Window          *Window          `json:"window,omitempty"`
	Fields          []string         `json:"fields,omitempty"`
	StrippedEmpty   bool             `json:"stripped_empty,omitempty"`
	ElidedFields    []string         `json:"elided_fields,omitempty"`
	Sort            string           `json:"sort,omitempty"`
	Dataset         *Dataset         `json:"dataset,omitempty"`
	Errors          []map[string]any `json:"errors,omitempty"`
	DryRun          bool             `json:"dry_run,omitempty"`
	Warnings        []Warning        `json:"warnings,omitempty"`
	Timing          *Timing          `json:"timing,omitempty"`
	ContractVersion string           `json:"contract_version"`
	ToolVersion     string           `json:"tool_version"`
}

// Page carries every member on every list response (contract §9.2); only the truncation
// details are optional.
type Page struct {
	Limit           int     `json:"limit"`
	Count           int     `json:"count"`
	Total           *int64  `json:"total"`
	TotalIsExact    bool    `json:"total_is_exact"`
	HasMore         bool    `json:"has_more"`
	NextCursor      *string `json:"next_cursor"`
	Truncated       bool    `json:"truncated"`
	TruncatedReason string  `json:"truncated_reason,omitempty"`
	Dropped         int     `json:"dropped,omitempty"`
}

type Window struct {
	Since  string `json:"since"`
	Until  string `json:"until"`
	Source string `json:"source,omitempty"`
}

type Dataset struct {
	ID               string `json:"id"`
	Path             string `json:"path,omitempty"`
	RecordCount      *int64 `json:"record_count,omitempty"`
	Bytes            *int64 `json:"bytes,omitempty"`
	Complete         bool   `json:"complete"`
	IncompleteReason string `json:"incomplete_reason,omitempty"`
	Offset           int64  `json:"offset"`
	Returned         int    `json:"returned"`
	TTLSeconds       int64  `json:"ttl_seconds"`
	ReadHint         string `json:"read_hint,omitempty"`
}

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Timing struct {
	TotalMs          int64 `json:"total_ms"`
	UpstreamRequests int   `json:"upstream_requests"`
	UpstreamMs       int64 `json:"upstream_ms"`
}

// New returns a success envelope with the version members set.
func New(tool, toolVersion string, command *string) *Envelope {
	return &Envelope{
		OK:      true,
		Tool:    tool,
		Command: command,
		Data:    shape.Null(),
		Meta:    Meta{ContractVersion: ContractVersion, ToolVersion: toolVersion},
	}
}

// SetError turns the envelope into a failure. data becomes null unless the caller sets it again
// afterwards (partial and doctor keep their data).
func (e *Envelope) SetError(err *errs.Error) {
	e.OK = false
	e.Data = shape.Null()
	e.Error = &ErrorBody{
		Code:         err.Code,
		ExitCode:     err.ExitCode(),
		Message:      limitMessage(err.Message),
		Retriable:    err.Retriable(),
		RetryAfterMs: err.RetryAfterMs,
		Hint:         err.Hint,
		Details:      limitDetails(err.Details),
	}
}

// ExitCode is the process status for this envelope.
func (e *Envelope) ExitCode() int {
	if e.Error == nil {
		return 0
	}
	return e.Error.ExitCode
}

func (e *Envelope) AddWarning(code, message string) {
	for _, existing := range e.Meta.Warnings {
		if existing.Code == code && existing.Message == message {
			return
		}
	}
	e.Meta.Warnings = append(e.Meta.Warnings, Warning{Code: code, Message: message})
}

// Encode serializes the envelope: compact with one trailing newline, or indented with two spaces.
func Encode(e *Envelope, pretty bool) []byte {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	if err := encoder.Encode(e); err != nil {
		// Only a defect can get here; report it without the payload that failed.
		fallback := New(e.Tool, e.Meta.ToolVersion, e.Command)
		fallback.SetError(errs.New(errs.Internal, "The response could not be encoded."))
		buffer.Reset()
		_ = encoder.Encode(fallback)
	}
	return buffer.Bytes()
}

func limitMessage(message string) string {
	if utf8.RuneCountInString(message) <= maxMessageChars {
		return message
	}
	runes := []rune(message)
	return string(runes[:maxMessageChars-1]) + "…"
}

// limitDetails keeps details within 2048 bytes by halving the largest list, or dropping the
// largest member, until it fits.
func limitDetails(details map[string]any) map[string]any {
	if len(details) == 0 {
		return nil
	}
	limited := make(map[string]any, len(details))
	for key, value := range details {
		limited[key] = value
	}
	for size(limited) > maxDetailsBytes {
		largest := ""
		largestSize := -1
		keys := make([]string, 0, len(limited))
		for key := range limited {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if memberSize := size(limited[key]); memberSize > largestSize {
				largest, largestSize = key, memberSize
			}
		}
		if list, ok := limited[largest].([]string); ok && len(list) > 1 {
			limited[largest] = list[:len(list)/2]
		} else {
			delete(limited, largest)
		}
		limited["details_truncated"] = true
	}
	return limited
}

func size(value any) int {
	encoded, _ := json.Marshal(value)
	return len(encoded)
}
