// Package errs defines the contract's closed set of error codes (contract §4) and the one error type
// every layer returns. Mapping observations to codes happens where the observation is made; mapping
// codes to exit statuses happens only here.
package errs

import (
	"errors"
	"fmt"
)

type Code string

const (
	Internal    Code = "internal"
	Usage       Code = "usage"
	Config      Code = "config"
	Auth        Code = "auth"
	NotFound    Code = "not_found"
	Validation  Code = "validation"
	Conflict    Code = "conflict"
	Refused     Code = "refused"
	RateLimited Code = "rate_limited"
	Timeout     Code = "timeout"
	Network     Code = "network"
	Upstream    Code = "upstream"
	Partial     Code = "partial"
	CacheMiss   Code = "cache_miss"
	Canceled    Code = "canceled"
)

var exitCodes = map[Code]int{
	Internal:    1,
	Usage:       2,
	Config:      3,
	Auth:        4,
	NotFound:    5,
	Validation:  6,
	Conflict:    7,
	Refused:     8,
	RateLimited: 9,
	Timeout:     10,
	Network:     11,
	Upstream:    12,
	Partial:     13,
	CacheMiss:   14,
	Canceled:    130,
}

var retriableByDefault = map[Code]bool{
	RateLimited: true,
	Timeout:     true,
	Network:     true,
	Upstream:    true,
	Partial:     true,
}

// Codes returns every code in exit-code order, for tests and discovery.
func Codes() []Code {
	return []Code{Internal, Usage, Config, Auth, NotFound, Validation, Conflict, Refused,
		RateLimited, Timeout, Network, Upstream, Partial, CacheMiss, Canceled}
}

// ExitCode returns the exit status paired with a code.
func ExitCode(code Code) int {
	if exit, ok := exitCodes[code]; ok {
		return exit
	}
	return exitCodes[Internal]
}

type Error struct {
	Code         Code
	Message      string
	Hint         string
	Details      map[string]any
	RetryAfterMs *int64
	retriable    *bool
	exitCode     int
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

func (e *Error) ExitCode() int {
	if e.exitCode != 0 {
		return e.exitCode
	}
	return ExitCode(e.Code)
}

func (e *Error) Retriable() bool {
	if e.retriable != nil {
		return *e.retriable
	}
	return retriableByDefault[e.Code]
}

func New(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

func (e *Error) WithHint(hint string) *Error {
	e.Hint = hint
	return e
}

func (e *Error) WithDetail(key string, value any) *Error {
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	e.Details[key] = value
	return e
}

func (e *Error) WithRetriable(retriable bool) *Error {
	e.retriable = &retriable
	return e
}

func (e *Error) WithRetryAfter(ms int64) *Error {
	e.RetryAfterMs = &ms
	return e
}

// CanceledBy returns the canceled error for the given signal exit status (130 or 143).
func CanceledBy(exitCode int) *Error {
	return &Error{Code: Canceled, Message: "Interrupted.", exitCode: exitCode}
}

// From converts any error into an *Error, treating anything unrecognised as internal.
func From(err error) *Error {
	if err == nil {
		return nil
	}
	var typed *Error
	if errors.As(err, &typed) {
		return typed
	}
	return New(Internal, "Unexpected failure: %s", err.Error())
}

func Usagef(format string, args ...any) *Error  { return New(Usage, format, args...) }
func Configf(format string, args ...any) *Error { return New(Config, format, args...) }
