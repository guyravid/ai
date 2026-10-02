package upstream

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
	"github.com/guyravid/ai/cli/tools/telegram/internal/shape"
)

const (
	maxAttempts      = 3
	initialBackoff   = 250 * time.Millisecond
	maxBackoff       = 4 * time.Second
	maxRedirects     = 3
	maxErrorBody     = 4096
	maxResponseBytes = 64 << 20
	redactedValue    = "***REDACTED***"
	// maxRateLimitWait is the longest a read waits out a 429 before retrying once. A longer
	// retry_after is reported to the caller instead.
	maxRateLimitWait = 30 * time.Second
	// tokenSegment stands in for the bot token in every URL shown to a person or written to a log.
	tokenSegment = "bot<redacted>"
)

// Protocol is the part of the HTTP exchange that belongs to the API's own conventions: how a
// successful body wraps its result, and how a failure maps onto the contract's codes.
type Protocol interface {
	// Result extracts the result from a 2xx body. It returns an error when the body reports failure.
	Result(status int, body []byte) (shape.Value, *errs.Error)
	// StatusError maps a non-2xx response. The wait is the Retry-After header, or 0.
	StatusError(status int, body []byte, wait time.Duration, request *Request) *errs.Error
}

// Logger receives debug events. Implementations write redacted JSON lines to stderr.
type Logger func(level, message string, fields map[string]any)

// Token supplies the bot token, or an error when it is missing or not usable for this profile. It
// is called only when a request is actually sent.
type Token func() (string, *errs.Error)

type Client struct {
	BaseURL       *url.URL
	Token         Token
	Protocol      Protocol
	Timeout       time.Duration
	UserAgent     string
	Deterministic bool
	Log           Logger
	HTTP          *http.Client

	Requests int
	Elapsed  time.Duration
	// LastDate is the Date header of the most recent response, for doctor's clock check.
	LastDate time.Time
	sleep    func(context.Context, time.Duration) error
}

// NewClient validates the base URL: an override is honoured only for loopback hosts, so a stray
// variable cannot send the credential to an arbitrary host (base template §7).
func NewClient(baseURL, defaultBaseURL string, protocol Protocol) (*Client, *errs.Error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return nil, errs.Configf("BASE_URL %q is not a valid URL.", baseURL)
	}
	if baseURL != defaultBaseURL && !isLoopback(parsed.Hostname()) {
		return nil, errs.Configf("BASE_URL %s is not the Telegram Bot API and not a loopback address; overrides are honoured only for loopback hosts.", baseURL).
			WithDetail("setting", "BASE_URL")
	}
	if parsed.Scheme != "https" && !isLoopback(parsed.Hostname()) {
		return nil, errs.Configf("BASE_URL must use https.")
	}
	client := &Client{BaseURL: parsed, Protocol: protocol, Timeout: 30 * time.Second, sleep: sleepContext}
	client.HTTP = &http.Client{
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout: 10 * time.Second,
			MaxIdleConns:        4,
			IdleConnTimeout:     30 * time.Second,
		},
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			if request.URL.Host != via[0].URL.Host {
				return fmt.Errorf("refused a redirect to another host (%s)", request.URL.Host)
			}
			return nil
		},
	}
	return client, nil
}

// SetSleep replaces the wait between retries, for tests.
func (c *Client) SetSleep(sleep func(context.Context, time.Duration) error) { c.sleep = sleep }

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// URL returns the URL of a request as it may be shown: the token segment is a placeholder. Request
// paths are already escaped segment by segment.
func (c *Client) URL(request *Request) string {
	return c.urlWith(request, tokenSegment)
}

func (c *Client) urlWith(request *Request, segment string) string {
	target := strings.TrimRight(c.BaseURL.String(), "/") + "/" + segment + request.Path
	if len(request.Query) > 0 {
		target += "?" + request.Query.Encode()
	}
	return target
}

// Preview describes the exact request that would be sent, with the token redacted (contract
// §11.2). The file content of an upload is never included.
func (c *Client) Preview(request *Request) shape.Value {
	var headers []shape.Field
	var body shape.Value
	switch {
	case request.Upload != nil:
		headers = append(headers, shape.Field{Key: "Content-Type", Value: shape.String("multipart/form-data")})
		file := []shape.Field{
			{Key: "path", Value: shape.String(request.Upload.Path)},
			{Key: "name", Value: shape.String(request.Upload.Name)},
		}
		if info, err := os.Stat(request.Upload.Path); err == nil && info.Mode().IsRegular() {
			file = append(file, shape.Field{Key: "bytes", Value: shape.Int(info.Size())})
		} else {
			file = append(file, shape.Field{Key: "readable", Value: shape.Bool(false)})
		}
		var fields []shape.Field
		for _, key := range sortedKeys(request.Upload.Fields) {
			fields = append(fields, shape.Field{Key: key, Value: shape.String(request.Upload.Fields[key])})
		}
		fields = append(fields, shape.Field{Key: request.Upload.Field, Value: shape.NewObject(file...)})
		body = shape.NewObject(fields...)
	case request.Body != nil:
		headers = append(headers, shape.Field{Key: "Content-Type", Value: shape.String("application/json")})
		keys := make([]string, 0, len(request.Body))
		for key := range request.Body {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var fields []shape.Field
		for _, key := range keys {
			value, err := shape.FromAny(request.Body[key])
			if err != nil {
				value = shape.String(fmt.Sprint(request.Body[key]))
			}
			fields = append(fields, shape.Field{Key: key, Value: value})
		}
		body = shape.NewObject(fields...)
	default:
		body = shape.Null()
	}
	return shape.NewObject(
		shape.Field{Key: "method", Value: shape.String(request.Method)},
		shape.Field{Key: "url", Value: shape.String(c.URL(request))},
		shape.Field{Key: "headers", Value: shape.NewObject(headers...)},
		shape.Field{Key: "body", Value: body},
	)
}

// Do sends a request and returns the parsed JSON response. Reads are retried; writes never are
// (contract §11.4).
func (c *Client) Do(ctx context.Context, request *Request) (shape.Value, error) {
	token, authErr := c.Token()
	if authErr != nil {
		return shape.Value{}, authErr
	}
	started := time.Now()
	defer func() { c.Elapsed += time.Since(started) }()

	attempts := maxAttempts
	if request.IsWrite() {
		attempts = 1
	}
	var lastErr *errs.Error
	rateLimitRetried := false
	for attempt := 1; attempt <= attempts; attempt++ {
		value, err := c.attempt(ctx, request, token)
		if err == nil {
			return value, nil
		}
		lastErr = err
		if attempt == attempts || !shouldRetry(err) {
			break
		}
		wait := c.backoff(attempt)
		if err.RetryAfterMs != nil {
			wait = time.Duration(*err.RetryAfterMs) * time.Millisecond
		}
		if err.Code == errs.RateLimited {
			// A throttled read waits out retry_after once; a longer wait goes back to the caller.
			if rateLimitRetried || wait > maxRateLimitWait {
				break
			}
			rateLimitRetried = true
		}
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < wait {
			break
		}
		if sleepErr := c.sleep(ctx, wait); sleepErr != nil {
			break
		}
	}
	return shape.Value{}, lastErr
}

func shouldRetry(err *errs.Error) bool {
	switch err.Code {
	case errs.RateLimited, errs.Network, errs.Upstream:
		return true
	case errs.Timeout:
		return err.Details["write_state"] == nil
	}
	return false
}

func (c *Client) backoff(attempt int) time.Duration {
	wait := initialBackoff << (attempt - 1)
	if wait > maxBackoff {
		wait = maxBackoff
	}
	if !c.Deterministic {
		wait += time.Duration(rand.Int64N(int64(wait / 2)))
	}
	return wait
}

func (c *Client) attempt(ctx context.Context, request *Request, token string) (shape.Value, *errs.Error) {
	timeout := c.Timeout
	if request.Timeout > 0 {
		timeout = request.Timeout
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpRequest, buildErr := c.build(requestCtx, request, token)
	if buildErr != nil {
		return shape.Value{}, buildErr
	}
	c.Requests++
	started := time.Now()
	response, err := c.HTTP.Do(httpRequest)
	if err != nil {
		mapped := c.transportError(ctx, requestCtx, request, err)
		c.logRequest(request, 0, time.Since(started), mapped)
		return shape.Value{}, mapped
	}
	defer response.Body.Close()
	if date, err := http.ParseTime(response.Header.Get("Date")); err == nil {
		c.LastDate = date
	}

	if response.StatusCode < 200 || response.StatusCode > 299 {
		excerpt, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
		mapped := c.Protocol.StatusError(response.StatusCode, excerpt, retryAfter(response), request)
		c.logRequest(request, response.StatusCode, time.Since(started), mapped)
		return shape.Value{}, mapped
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return shape.Value{}, c.transportError(ctx, requestCtx, request, err)
	}
	value, resultErr := c.Protocol.Result(response.StatusCode, content)
	c.logRequest(request, response.StatusCode, time.Since(started), resultErr)
	if resultErr != nil {
		return shape.Value{}, resultErr
	}
	return value, nil
}

func (c *Client) build(ctx context.Context, request *Request, token string) (*http.Request, *errs.Error) {
	var body io.Reader
	contentType := ""
	switch {
	case request.Upload != nil:
		reader, kind, err := multipartBody(request.Upload)
		if err != nil {
			return nil, err
		}
		body, contentType = reader, kind
	case request.Body != nil:
		encoded, _ := json.Marshal(request.Body)
		body, contentType = bytes.NewReader(encoded), "application/json"
	}
	httpRequest, err := http.NewRequestWithContext(ctx, request.Method, c.urlWith(request, "bot"+token), body)
	if err != nil {
		// The URL carries the token, so the parse error is not repeated.
		return nil, errs.New(errs.Internal, "The request could not be built.")
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("User-Agent", c.UserAgent)
	if contentType != "" {
		httpRequest.Header.Set("Content-Type", contentType)
	}
	return httpRequest, nil
}

// multipartBody reads the file only now, when the write has been confirmed.
func multipartBody(upload *Upload) (io.Reader, string, *errs.Error) {
	info, err := os.Stat(upload.Path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, "", errs.Usagef("The file %s cannot be read, or is not a regular file.", upload.Path).
			WithDetail("file", upload.Path)
	}
	file, err := os.Open(upload.Path)
	if err != nil {
		return nil, "", errs.Usagef("The file %s cannot be opened.", upload.Path).WithDetail("file", upload.Path)
	}
	defer file.Close()
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for _, key := range sortedKeys(upload.Fields) {
		_ = writer.WriteField(key, upload.Fields[key])
	}
	part, err := writer.CreateFormFile(upload.Field, upload.Name)
	if err == nil {
		_, err = io.Copy(part, file)
	}
	if err != nil {
		return nil, "", errs.Usagef("The file %s could not be read.", upload.Path).WithDetail("file", upload.Path)
	}
	_ = writer.Close()
	return &buffer, writer.FormDataContentType(), nil
}

func (c *Client) transportError(parent, requestCtx context.Context, request *Request, err error) *errs.Error {
	host := c.BaseURL.Hostname()
	timedOut := errors.Is(err, context.DeadlineExceeded) || errors.Is(requestCtx.Err(), context.DeadlineExceeded)
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		timedOut = true
	}
	if errors.Is(parent.Err(), context.Canceled) {
		return errs.New(errs.Canceled, "Interrupted.")
	}
	if timedOut {
		if request.IsWrite() {
			return errs.New(errs.Timeout, "No response from Telegram in time; the message may or may not have been sent.").
				WithRetriable(false).WithDetail("write_state", "unknown").
				WithHint("Check the chat before trying again.")
		}
		message := fmt.Sprintf("Telegram did not respond within %s.", c.Timeout)
		if errors.Is(parent.Err(), context.DeadlineExceeded) {
			message = "The invocation's time budget ran out before Telegram responded."
		}
		return errs.New(errs.Timeout, "%s", message)
	}
	// err.Error() embeds the request URL, and the token is in its path, so nothing below repeats it
	// except through MaskToken.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return errs.New(errs.Network, "Could not resolve %s.", host).WithHint(ToolName+" doctor").
			WithDetail("cause", MaskToken(err.Error()))
	}
	var tlsErr *tls.CertificateVerificationError
	if errors.As(err, &tlsErr) {
		return errs.New(errs.Network, "TLS verification failed for %s.", host).WithHint(ToolName+" doctor").
			WithDetail("cause", MaskToken(err.Error()))
	}
	if strings.Contains(err.Error(), "redirect") {
		return errs.New(errs.Upstream, "Telegram redirected the request somewhere it cannot follow.").WithRetriable(false)
	}
	return errs.New(errs.Network, "Could not connect to %s.", host).WithHint(ToolName+" doctor").
		WithDetail("cause", MaskToken(err.Error()))
}

// ToolName is used in hints.
const ToolName = "telegram"

var tokenInText = regexp.MustCompile(`/bot[^/\s"']+`)

// MaskToken replaces the token segment of any /bot<token> path in text. Go's HTTP errors embed the
// full request URL, so every transport error string passes through here before it is shown; the
// response redactor then catches anything this misses.
func MaskToken(text string) string {
	return tokenInText.ReplaceAllString(text, "/"+tokenSegment)
}

func retryAfter(response *http.Response) time.Duration {
	value := response.Header.Get("Retry-After")
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		return max(time.Until(when), 0)
	}
	return 0
}

func (c *Client) logRequest(request *Request, status int, elapsed time.Duration, err *errs.Error) {
	if c.Log == nil {
		return
	}
	fields := map[string]any{
		"method": request.Method,
		"url":    MaskURL(c.URL(request)),
		"status": status,
		"ms":     elapsed.Milliseconds(),
	}
	if err != nil {
		fields["error"] = string(err.Code)
	}
	c.Log("debug", "upstream request", fields)
}

var secretParamWords = []string{"key", "token", "password", "secret", "signature", "auth"}

// MaskURL masks query parameters whose names suggest secrets (contract §12.3).
func MaskURL(raw string) string {
	raw = MaskToken(raw)
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query := parsed.Query()
	for name := range query {
		lower := strings.ToLower(name)
		for _, word := range secretParamWords {
			if strings.Contains(lower, word) {
				query.Set(name, redactedValue)
			}
		}
	}
	parsed.RawQuery = query.Encode()
	return MaskToken(parsed.String())
}

func sleepContext(ctx context.Context, wait time.Duration) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
