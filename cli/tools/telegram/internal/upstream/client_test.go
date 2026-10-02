package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
	"github.com/guyravid/ai/cli/tools/telegram/internal/shape"
)

const (
	defaultBase = "https://api.telegram.org"
	fakeToken   = "123456789:AAFakeTokenForTestsOnly0123456789abcd"
)

// testProtocol is the smallest Protocol that exercises the client: 429 carries Retry-After into
// RetryAfterMs, 5xx is upstream, 4xx is validation. The Telegram mapping is tested in its own package.
type testProtocol struct{}

func (testProtocol) Result(_ int, body []byte) (shape.Value, *errs.Error) {
	value, err := shape.Parse(body)
	if err != nil {
		return shape.Value{}, errs.New(errs.Upstream, "not JSON")
	}
	return value, nil
}

func (testProtocol) StatusError(status int, _ []byte, wait time.Duration, _ *Request) *errs.Error {
	switch {
	case status == 429:
		err := errs.New(errs.RateLimited, "throttled")
		if wait > 0 {
			err.WithRetryAfter(wait.Milliseconds())
		}
		return err
	case status >= 500:
		return errs.New(errs.Upstream, "upstream %d", status)
	}
	return errs.New(errs.Validation, "rejected %d", status)
}

// scripted answers each request with the next status in the script, then 200 with {"ok":true}.
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
	fmt.Fprint(w, `{"ok":true}`)
}

// newTestClient points a client at handler and records every backoff wait instead of sleeping.
func newTestClient(t *testing.T, handler http.Handler) (*Client, *[]time.Duration) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, defaultBase, testProtocol{})
	if err != nil {
		t.Fatal(err)
	}
	client.Token = func() (string, *errs.Error) { return fakeToken, nil }
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

// Telegram takes reads as POST too, so a POST that is not marked Write is retried like any read.
func TestRetriesTransientReadFailuresWithBackoff(t *testing.T) {
	handler := &scripted{statuses: []int{503, 502}}
	client, waits := newTestClient(t, handler)
	if _, err := client.Do(context.Background(), &Request{Method: "POST", Path: "/getMe"}); err != nil {
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
	_, err := client.Do(context.Background(), &Request{Method: "POST", Path: "/getMe"})
	if typed := asError(t, err); typed.Code != errs.Upstream || handler.calls.Load() != maxAttempts {
		t.Errorf("code %s after %d calls", typed.Code, handler.calls.Load())
	}
}

// A throttled read waits out retry_after once, then reports rate_limited with the wait (plan, error
// mapping): never a second retry.
func TestRateLimitedReadRetriesOnceThenReportsRetryAfter(t *testing.T) {
	handler := &scripted{statuses: []int{429, 429, 429}, headers: map[string]string{"Retry-After": "2"}}
	client, waits := newTestClient(t, handler)
	_, err := client.Do(context.Background(), &Request{Method: "POST", Path: "/getMe"})
	typed := asError(t, err)
	if typed.Code != errs.RateLimited || typed.RetryAfterMs == nil || *typed.RetryAfterMs != 2000 {
		t.Errorf("code %s retry_after_ms %v", typed.Code, typed.RetryAfterMs)
	}
	if handler.calls.Load() != 2 {
		t.Errorf("%d calls, want exactly one retry", handler.calls.Load())
	}
	if fmt.Sprint(*waits) != fmt.Sprint([]time.Duration{2 * time.Second}) {
		t.Errorf("waits %v, want one wait of retry_after", *waits)
	}
}

func TestRateLimitedReadSucceedsOnTheRetry(t *testing.T) {
	handler := &scripted{statuses: []int{429}, headers: map[string]string{"Retry-After": "1"}}
	client, waits := newTestClient(t, handler)
	if _, err := client.Do(context.Background(), &Request{Method: "POST", Path: "/getMe"}); err != nil {
		t.Fatal(err)
	}
	if handler.calls.Load() != 2 || len(*waits) != 1 {
		t.Errorf("%d calls, waits %v", handler.calls.Load(), *waits)
	}
}

func TestRateLimitedWaitBeyondThirtySecondsIsNotTaken(t *testing.T) {
	handler := &scripted{statuses: []int{429, 429}, headers: map[string]string{"Retry-After": "31"}}
	client, waits := newTestClient(t, handler)
	_, err := client.Do(context.Background(), &Request{Method: "POST", Path: "/getMe"})
	typed := asError(t, err)
	if typed.Code != errs.RateLimited || *typed.RetryAfterMs != 31000 || handler.calls.Load() != 1 || len(*waits) != 0 {
		t.Errorf("code %s, %d calls, waits %v", typed.Code, handler.calls.Load(), *waits)
	}
}

func TestRateLimitedWithoutRetryAfterUsesBackoffOnce(t *testing.T) {
	handler := &scripted{statuses: []int{429, 429, 429}}
	client, waits := newTestClient(t, handler)
	_, err := client.Do(context.Background(), &Request{Method: "POST", Path: "/getMe"})
	typed := asError(t, err)
	if typed.Code != errs.RateLimited || typed.RetryAfterMs != nil || handler.calls.Load() != 2 {
		t.Errorf("code %s retry_after_ms %v after %d calls", typed.Code, typed.RetryAfterMs, handler.calls.Load())
	}
	if fmt.Sprint(*waits) != fmt.Sprint([]time.Duration{250 * time.Millisecond}) {
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
	for _, statuses := range [][]int{{503, 503}, {429, 429}} {
		handler := &scripted{statuses: statuses, headers: map[string]string{"Retry-After": "1"}}
		client, waits := newTestClient(t, handler)
		_, err := client.Do(context.Background(), &Request{Method: "POST", Path: "/sendMessage", Write: true,
			Body: map[string]any{"chat_id": 1, "text": "x"}})
		typed := asError(t, err)
		if handler.calls.Load() != 1 || len(*waits) != 0 {
			t.Errorf("write answered %v: %d calls, waits %v", statuses, handler.calls.Load(), *waits)
		}
		if statuses[0] == 429 && (typed.Code != errs.RateLimited || typed.RetryAfterMs == nil) {
			t.Errorf("a throttled write still reports retry_after_ms: %+v", typed)
		}
	}
}

func TestWriteTimeoutHasUnknownState(t *testing.T) {
	var calls atomic.Int32
	client, waits := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(300 * time.Millisecond)
	}))
	client.Timeout = 50 * time.Millisecond
	_, err := client.Do(context.Background(), &Request{Method: "POST", Path: "/sendMessage", Write: true,
		Body: map[string]any{"chat_id": 1, "text": "x"}})
	typed := asError(t, err)
	if typed.Code != errs.Timeout || typed.Details["write_state"] != "unknown" || typed.Retriable() || calls.Load() != 1 || len(*waits) != 0 {
		t.Errorf("got %+v after %d calls", typed, calls.Load())
	}
}

func TestPermanentErrorsAreNotRetried(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 409} {
		handler := &scripted{statuses: []int{status, status}}
		client, waits := newTestClient(t, handler)
		_, err := client.Do(context.Background(), &Request{Method: "POST", Path: "/getMe"})
		if typed := asError(t, err); typed.Code != errs.Validation || handler.calls.Load() != 1 || len(*waits) != 0 {
			t.Errorf("%d: code %s after %d calls", status, typed.Code, handler.calls.Load())
		}
	}
}

func TestDeadlineStopsRetrying(t *testing.T) {
	handler := &scripted{statuses: []int{429, 429, 429}, headers: map[string]string{"Retry-After": "20"}}
	client, waits := newTestClient(t, handler)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := client.Do(ctx, &Request{Method: "POST", Path: "/getMe"})
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
		"http://127.0.0.1:9999":        true,
		"http://localhost:9999":        true,
		"http://[::1]:9999":            true,
		"https://evil.example":         false,
		"http://api.telegram.org":      false, // not the default, and not loopback
		"https://api.telegram.org.bad": false,
		"not a url":                    false,
	}
	for baseURL, allowed := range cases {
		_, err := NewClient(baseURL, defaultBase, testProtocol{})
		if (err == nil) != allowed {
			t.Errorf("%s: allowed=%v, err=%v", baseURL, allowed, err)
		}
		if err != nil && err.Code != errs.Config {
			t.Errorf("%s: code %s, want config", baseURL, err.Code)
		}
	}
}

// The token travels in the URL path and nowhere else: no header, and the URL shown to people has a
// placeholder.
func TestTokenIsInThePathOnly(t *testing.T) {
	var path, headers string
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		for name, values := range r.Header {
			headers += name + "=" + strings.Join(values, ",") + ";"
		}
		fmt.Fprint(w, `{"ok":true}`)
	}))
	request := &Request{Method: "POST", Path: "/getMe"}
	if _, err := client.Do(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if path != "/bot"+fakeToken+"/getMe" {
		t.Errorf("path %q", path)
	}
	if strings.Contains(headers, fakeToken) || strings.Contains(strings.ToLower(headers), "authorization") {
		t.Errorf("the token must not be in a header: %s", headers)
	}
	if shown := client.URL(request); strings.Contains(shown, fakeToken) || !strings.HasSuffix(shown, "/bot<redacted>/getMe") {
		t.Errorf("shown URL %q", shown)
	}
}

func TestPreviewRedactsTokenAndShowsBody(t *testing.T) {
	client, _ := newTestClient(t, http.NotFoundHandler())
	preview := client.Preview(&Request{Method: "POST", Path: "/sendMessage", Write: true,
		Body: map[string]any{"text": "hi", "chat_id": 111, "disable_notification": true}})
	text := string(preview.Marshal())
	if strings.Contains(text, fakeToken) || strings.Contains(text, fakeToken[:12]) {
		t.Fatalf("preview leaks the token: %s", text)
	}
	for _, want := range []string{`"bot<redacted>/sendMessage"`, `"chat_id":111`, `"disable_notification":true`, `"text":"hi"`} {
		if want == `"bot<redacted>/sendMessage"` {
			want = `/bot<redacted>/sendMessage"`
		}
		if !strings.Contains(text, want) {
			t.Errorf("preview lacks %s: %s", want, text)
		}
	}
}

// Go's transport errors embed the full URL, which holds the token. Whatever reaches the caller is
// masked (plan, trap 1).
func TestTransportErrorsDoNotContainTheToken(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close() // nothing listens any more: the dial fails
	client, err := NewClient(url, defaultBase, testProtocol{})
	if err != nil {
		t.Fatal(err)
	}
	client.Token = func() (string, *errs.Error) { return fakeToken, nil }
	client.sleep = func(context.Context, time.Duration) error { return nil }
	_, doErr := client.Do(context.Background(), &Request{Method: "POST", Path: "/getMe"})
	typed := asError(t, doErr)
	if typed.Code != errs.Network {
		t.Fatalf("code %s, want network", typed.Code)
	}
	encoded, _ := json.Marshal(typed.Details)
	for _, text := range []string{typed.Message, typed.Hint, string(encoded), typed.Error()} {
		if strings.Contains(text, fakeToken) || strings.Contains(text, fakeToken[:12]) || strings.Contains(text, "AAFakeToken") {
			t.Errorf("token leaked in %q", text)
		}
	}
	if cause, _ := typed.Details["cause"].(string); !strings.Contains(cause, "/bot<redacted>/getMe") {
		t.Errorf("cause should keep the masked path, got %q", cause)
	}
}

func TestMaskToken(t *testing.T) {
	text := `Post "https://api.telegram.org/bot` + fakeToken + `/sendMessage": dial tcp: lookup api.telegram.org: no such host`
	got := MaskToken(text)
	if strings.Contains(got, fakeToken) || !strings.Contains(got, `/bot<redacted>/sendMessage"`) || !strings.Contains(got, "no such host") {
		t.Errorf("got %q", got)
	}
}

// Logged URLs mask the token path segment and query parameters whose names suggest secrets
// (contract §12.3).
func TestMaskURL(t *testing.T) {
	got := MaskURL("https://api.telegram.org/bot" + fakeToken + "/getUpdates?key=K1&token=T1&filter=open&X-Signature=S&query=token%3Dabc")
	for _, secret := range []string{fakeToken, "K1", "T1", "=S"} {
		if strings.Contains(got, secret) {
			t.Errorf("%s survived in %s", secret, got)
		}
	}
	for _, kept := range []string{"/bot<redacted>/getUpdates", "filter=open", "query=token%3Dabc"} {
		if !strings.Contains(got, kept) {
			t.Errorf("%s was masked in %s", kept, got)
		}
	}
}

func TestVerboseLogNeverCarriesTheToken(t *testing.T) {
	client, _ := newTestClient(t, &scripted{})
	var lines []string
	client.Log = func(level, message string, fields map[string]any) {
		encoded, _ := json.Marshal(fields)
		lines = append(lines, message+string(encoded))
	}
	if _, err := client.Do(context.Background(), &Request{Method: "POST", Path: "/getMe"}); err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 {
		t.Fatal("expected a log line")
	}
	for _, line := range lines {
		if strings.Contains(line, fakeToken) || !strings.Contains(line, "redacted") {
			t.Errorf("log line %s", line)
		}
	}
}

func TestMissingTokenSendsNothing(t *testing.T) {
	handler := &scripted{}
	client, _ := newTestClient(t, handler)
	client.Token = func() (string, *errs.Error) { return "", errs.New(errs.Auth, "No bot token is configured.") }
	_, err := client.Do(context.Background(), &Request{Method: "POST", Path: "/getMe"})
	if asError(t, err).Code != errs.Auth || handler.calls.Load() != 0 {
		t.Errorf("a missing token must fail before any request: %d calls", handler.calls.Load())
	}
}
