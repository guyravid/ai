package upstream

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
)

const defaultBase = "https://api.trello.com/1"

// scripted answers each request with the next status in the script, then 200 with [].
type scripted struct {
	statuses []int
	headers  map[string]string
	calls    atomic.Int32
}

func (s *scripted) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	index := int(s.calls.Add(1)) - 1
	for key, value := range s.headers {
		w.Header().Set(key, value)
	}
	if index < len(s.statuses) {
		w.WriteHeader(s.statuses[index])
		fmt.Fprint(w, "nope")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `[]`)
}

// newTestClient points a client at handler and records every backoff wait instead of sleeping.
func newTestClient(t *testing.T, handler http.Handler) (*Client, *[]time.Duration) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL+"/1", defaultBase)
	if err != nil {
		t.Fatal(err)
	}
	client.Credentials = func() (string, *errs.Error) { return "OAuth test", nil }
	client.Deterministic = true
	waits := &[]time.Duration{}
	client.sleep = func(_ context.Context, wait time.Duration) error {
		*waits = append(*waits, wait)
		return nil
	}
	return client, waits
}

func asError(t *testing.T, err error) *errs.Error {
	t.Helper()
	typed, ok := err.(*errs.Error)
	if !ok {
		t.Fatalf("got %T %v, want *errs.Error", err, err)
	}
	return typed
}

func TestRetriesTransientFailuresWithBackoff(t *testing.T) {
	handler := &scripted{statuses: []int{503, 502}}
	client, waits := newTestClient(t, handler)
	if _, err := client.Do(context.Background(), &Request{Method: "GET", Path: "/boards"}); err != nil {
		t.Fatal(err)
	}
	if handler.calls.Load() != 3 || client.Requests != 3 {
		t.Errorf("calls %d, requests %d, want 3", handler.calls.Load(), client.Requests)
	}
	want := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond}
	if fmt.Sprint(*waits) != fmt.Sprint(want) {
		t.Errorf("waits %v, want %v", *waits, want)
	}
}

func TestGivesUpAfterMaxAttempts(t *testing.T) {
	handler := &scripted{statuses: []int{500, 500, 500, 500}}
	client, _ := newTestClient(t, handler)
	_, err := client.Do(context.Background(), &Request{Method: "GET", Path: "/boards"})
	if typed := asError(t, err); typed.Code != errs.Upstream || handler.calls.Load() != maxAttempts {
		t.Errorf("code %s after %d calls", typed.Code, handler.calls.Load())
	}
}

func TestRateLimitedHonoursRetryAfterSeconds(t *testing.T) {
	handler := &scripted{statuses: []int{429, 429, 429}, headers: map[string]string{"Retry-After": "2"}}
	client, waits := newTestClient(t, handler)
	_, err := client.Do(context.Background(), &Request{Method: "GET", Path: "/boards"})
	typed := asError(t, err)
	if typed.Code != errs.RateLimited || typed.RetryAfterMs == nil || *typed.RetryAfterMs != 2000 {
		t.Errorf("code %s retry_after_ms %v", typed.Code, typed.RetryAfterMs)
	}
	if fmt.Sprint(*waits) != fmt.Sprint([]time.Duration{2 * time.Second, 2 * time.Second}) {
		t.Errorf("waits %v, want Retry-After each time", *waits)
	}
}

func TestRateLimitedWithoutRetryAfterUsesBackoff(t *testing.T) {
	handler := &scripted{statuses: []int{429, 429, 429}}
	client, waits := newTestClient(t, handler)
	_, err := client.Do(context.Background(), &Request{Method: "GET", Path: "/boards"})
	typed := asError(t, err)
	if typed.Code != errs.RateLimited || typed.RetryAfterMs != nil {
		t.Errorf("code %s retry_after_ms %v", typed.Code, typed.RetryAfterMs)
	}
	if fmt.Sprint(*waits) != fmt.Sprint([]time.Duration{250 * time.Millisecond, 500 * time.Millisecond}) {
		t.Errorf("waits %v", *waits)
	}
}

func TestRetryAfterHTTPDate(t *testing.T) {
	response := &http.Response{Header: http.Header{}}
	response.Header.Set("Retry-After", time.Now().Add(90*time.Second).UTC().Format(http.TimeFormat))
	if wait := retryAfter(response); wait < 80*time.Second || wait > 90*time.Second {
		t.Errorf("HTTP-date Retry-After gave %v", wait)
	}
	response.Header.Set("Retry-After", time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat))
	if wait := retryAfter(response); wait != 0 {
		t.Errorf("a past date gave %v", wait)
	}
	response.Header.Set("Retry-After", "soon")
	if wait := retryAfter(response); wait != 0 {
		t.Errorf("garbage gave %v", wait)
	}
}

func TestWritesAreNeverRetried(t *testing.T) {
	handler := &scripted{statuses: []int{503, 503}}
	client, waits := newTestClient(t, handler)
	_, err := client.Do(context.Background(), &Request{Method: "POST", Path: "/cards", Body: map[string]string{"name": "x"}})
	if asError(t, err).Code != errs.Upstream || handler.calls.Load() != 1 || len(*waits) != 0 {
		t.Errorf("write: %d calls, waits %v", handler.calls.Load(), *waits)
	}
}

func TestWriteTimeoutHasUnknownState(t *testing.T) {
	handler := &scripted{statuses: []int{504}}
	client, _ := newTestClient(t, handler)
	_, err := client.Do(context.Background(), &Request{Method: "PUT", Path: "/cards/1", Body: map[string]string{"desc": "x"}})
	typed := asError(t, err)
	if typed.Code != errs.Timeout || typed.Details["write_state"] != "unknown" || typed.Retriable() {
		t.Errorf("got %+v", typed)
	}
}

func TestPermanentErrorsAreNotRetried(t *testing.T) {
	for status, code := range map[int]errs.Code{404: errs.NotFound, 401: errs.Auth, 400: errs.Validation, 409: errs.Conflict} {
		handler := &scripted{statuses: []int{status, status}}
		client, waits := newTestClient(t, handler)
		_, err := client.Do(context.Background(), &Request{Method: "GET", Path: "/cards/x"})
		if typed := asError(t, err); typed.Code != code || handler.calls.Load() != 1 || len(*waits) != 0 {
			t.Errorf("%d: code %s after %d calls", status, typed.Code, handler.calls.Load())
		}
	}
}

func TestDeadlineStopsRetrying(t *testing.T) {
	handler := &scripted{statuses: []int{429, 429, 429}, headers: map[string]string{"Retry-After": "30"}}
	client, waits := newTestClient(t, handler)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := client.Do(ctx, &Request{Method: "GET", Path: "/boards"})
	if asError(t, err).Code != errs.RateLimited || handler.calls.Load() != 1 || len(*waits) != 0 {
		t.Errorf("a wait past the deadline must not be taken: %d calls, waits %v", handler.calls.Load(), *waits)
	}
}

func TestBackoffJitterIsBounded(t *testing.T) {
	client := &Client{}
	for attempt := 1; attempt <= 6; attempt++ {
		base := min(initialBackoff<<(attempt-1), maxBackoff)
		for range 50 {
			if wait := client.backoff(attempt); wait < base || wait >= base+base/2 {
				t.Fatalf("attempt %d: wait %v outside [%v, %v)", attempt, wait, base, base+base/2)
			}
		}
	}
	client.Deterministic = true
	if client.backoff(10) != maxBackoff {
		t.Error("backoff must cap at maxBackoff")
	}
}

func TestBaseURLOverrideOnlyForLoopback(t *testing.T) {
	cases := map[string]bool{
		defaultBase:                    true,
		"http://127.0.0.1:9999/1":      true,
		"http://localhost:9999/1":      true,
		"http://[::1]:9999/1":          true,
		"https://evil.example/1":       false,
		"http://api.trello.com/1":      false, // not the default, and not loopback
		"https://api.trello.com.evil/": false,
		"not a url":                    false,
	}
	for baseURL, allowed := range cases {
		_, err := NewClient(baseURL, defaultBase)
		if (err == nil) != allowed {
			t.Errorf("%s: allowed=%v, err=%v", baseURL, allowed, err)
		}
		if err != nil && err.Code != errs.Config {
			t.Errorf("%s: code %s, want config", baseURL, err.Code)
		}
	}
}

func TestCredentialsNeverInURL(t *testing.T) {
	var seen string
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.String() + " " + r.Header.Get("Authorization")
		fmt.Fprint(w, `[]`)
	}))
	client.Credentials = func() (string, *errs.Error) { return `OAuth oauth_consumer_key="K", oauth_token="T"`, nil }
	if _, err := client.Do(context.Background(), &Request{Method: "GET", Path: "/boards"}); err != nil {
		t.Fatal(err)
	}
	if seen != `/1/boards OAuth oauth_consumer_key="K", oauth_token="T"` {
		t.Errorf("request was %q", seen)
	}
}

// Logged URLs mask query parameters whose names suggest secrets (contract §12.3, C12-6).
func TestMaskURL(t *testing.T) {
	got := MaskURL("https://api.trello.com/1/boards?key=K1&token=T1&filter=open&X-Signature=S&oauth_token=O&query=token%3Dabc")
	for _, secret := range []string{"K1", "T1", "=S", "=O"} {
		if strings.Contains(got, secret) {
			t.Errorf("%s survived in %s", secret, got)
		}
	}
	for _, kept := range []string{"filter=open", "query=token%3Dabc"} {
		if !strings.Contains(got, kept) {
			t.Errorf("%s was masked in %s", kept, got)
		}
	}
}

// Upstream text copied into details is cut to 512 characters (contract §3.3, C3-12).
func TestUpstreamExcerptIsCut(t *testing.T) {
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		fmt.Fprint(w, strings.Repeat("é", 3000))
	}))
	_, err := client.Do(context.Background(), &Request{Method: "GET", Path: "/boards"})
	excerpt, _ := asError(t, err).Details["upstream_excerpt"].(string)
	if count := len([]rune(excerpt)); count != 512 {
		t.Errorf("excerpt is %d characters, want 512", count)
	}
}

// An oversize upload is rejected content, not a transient failure: validation, never retried.
func TestTooLargeIsValidation(t *testing.T) {
	handler := &scripted{statuses: []int{413, 413, 413}}
	client, waits := newTestClient(t, handler)
	_, err := client.Do(context.Background(), &Request{Method: "GET", Path: "/boards"})
	typed := asError(t, err)
	if typed.Code != errs.Validation || typed.Retriable() || handler.calls.Load() != 1 || len(*waits) != 0 {
		t.Errorf("code %s retriable %v after %d calls", typed.Code, typed.Retriable(), handler.calls.Load())
	}
}
