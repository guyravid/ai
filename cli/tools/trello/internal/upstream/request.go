// Package upstream is the HTTP layer: requests, retries, deadlines, redirects, and the mapping of
// upstream failures onto the contract's error codes.
package upstream

import (
	"net/url"
)

// Request is an upstream call built before anything is sent, so a write's preview is exactly what
// would go out (base template §13).
type Request struct {
	Method string
	Path   string            // relative to BASE_URL, such as "/cards"
	Query  url.Values        // never carries credentials; they travel in the Authorization header
	Body   map[string]string // sent as a JSON object
	Upload *Upload           // multipart body with one file part; exclusive with Body
}

// Upload is a file attached as multipart/form-data. The content is read only when sending.
type Upload struct {
	Field  string            // multipart field name, "file" for Trello
	Path   string            // absolute, symlinks resolved
	Name   string            // file name sent upstream
	Fields map[string]string // other form fields sent alongside the file
}

// IsWrite reports whether the request can change upstream state.
func (r *Request) IsWrite() bool {
	switch r.Method {
	case "GET", "HEAD", "OPTIONS":
		return false
	}
	return true
}
