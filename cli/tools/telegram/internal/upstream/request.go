// Package upstream is the HTTP layer: requests, retries, deadlines, redirects, and the mapping of
// upstream failures onto the contract's error codes.
package upstream

import (
	"net/url"
	"time"
)

// Request is an upstream call built before anything is sent, so a write's preview is exactly what
// would go out (base template §13).
//
// Telegram takes every Bot API call as a POST, reads included, so whether a request changes state
// is declared in Write rather than inferred from the method.
type Request struct {
	Method string
	Path   string         // the Bot API method, such as "/sendMessage"; the token segment is added when sending
	Query  url.Values     // never carries the token; it travels in the URL path
	Body   map[string]any // sent as a JSON object
	Upload *Upload        // multipart body with one file part; exclusive with Body
	// Timeout overrides the client's per-request deadline when positive, for a long poll that must
	// outlast the server's wait.
	Timeout time.Duration
	// Write marks a request that changes upstream state. Writes are never retried (contract §11.4).
	Write bool
}

// Upload is a file sent as multipart/form-data. The content is read only when sending.
type Upload struct {
	Field  string            // multipart field name, such as "document"
	Path   string            // absolute, symlinks resolved
	Name   string            // file name sent upstream
	Fields map[string]string // other form fields sent alongside the file
}

// IsWrite reports whether the request can change upstream state.
func (r *Request) IsWrite() bool { return r.Write }
