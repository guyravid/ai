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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
	"github.com/guyravid/ai/cli/tools/trello/internal/shape"
)

const (
	maxAttempts      = 3
	initialBackoff   = 250 * time.Millisecond
	maxBackoff       = 4 * time.Second
	maxRedirects     = 3
	maxErrorBody     = 4096
	maxExcerptChars  = 512
	maxResponseBytes = 64 << 20
	redactedValue    = "***REDACTED***"
)

// Logger receives debug events. Implementations write redacted JSON lines to stderr.
type Logger func(level, message string, fields map[string]any)

// Credentials supplies the Authorization header value, or an auth error when a credential is
// missing. It is called only when a request is actually sent.
type Credentials func() (string, *errs.Error)

type Client struct {
	BaseURL       *url.URL
	Credentials   Credentials
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
func NewClient(baseURL, defaultBaseURL string) (*Client, *errs.Error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return nil, errs.Configf("BASE_URL %q is not a valid URL.", baseURL)
	}
	if baseURL != defaultBaseURL && !isLoopback(parsed.Hostname()) {
		return nil, errs.Configf("BASE_URL %s is not the Trello API and not a loopback address; overrides are honoured only for loopback hosts.", baseURL).
			WithDetail("setting", "BASE_URL")
	}
	if parsed.Scheme != "https" && !isLoopback(parsed.Hostname()) {
		return nil, errs.Configf("BASE_URL must use https.")
	}
	client := &Client{BaseURL: parsed, Timeout: 30 * time.Second, sleep: sleepContext}
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

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// URL returns the full URL for a request, without credentials. Request paths are already escaped
// segment by segment.
func (c *Client) URL(request *Request) string {
	target := strings.TrimRight(c.BaseURL.String(), "/") + request.Path
	if len(request.Query) > 0 {
		target += "?" + request.Query.Encode()
	}
	return target
}

// Preview describes the exact request that would be sent, with header values redacted
// (contract §11.2). The file content of an upload is never included.
func (c *Client) Preview(request *Request) shape.Value {
	headers := []shape.Field{{Key: "Authorization", Value: shape.String(redactedValue)}}
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
		fields := []shape.Field{{Key: request.Upload.Field, Value: shape.NewObject(file...)}}
		for _, key := range sortedKeys(request.Upload.Fields) {
			fields = append(fields, shape.Field{Key: key, Value: shape.String(request.Upload.Fields[key])})
		}
		body = shape.NewObject(fields...)
	case request.Body != nil:
		headers = append(headers, shape.Field{Key: "Content-Type", Value: shape.String("application/json")})
		var fields []shape.Field
		for _, key := range sortedKeys(request.Body) {
			fields = append(fields, shape.Field{Key: key, Value: shape.String(request.Body[key])})
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
	header, authErr := c.Credentials()
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
	for attempt := 1; attempt <= attempts; attempt++ {
		value, err, retryAfter := c.attempt(ctx, request, header)
		if err == nil {
			return value, nil
		}
		lastErr = err
		if attempt == attempts || !shouldRetry(err) {
			break
		}
		wait := c.backoff(attempt)
		if retryAfter > 0 {
			wait = retryAfter
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

func (c *Client) attempt(ctx context.Context, request *Request, header string) (shape.Value, *errs.Error, time.Duration) {
	requestCtx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	httpRequest, buildErr := c.build(requestCtx, request, header)
	if buildErr != nil {
		return shape.Value{}, buildErr, 0
	}
	c.Requests++
	started := time.Now()
	response, err := c.HTTP.Do(httpRequest)
	if err != nil {
		mapped := c.transportError(ctx, requestCtx, request, err)
		c.logRequest(request, 0, time.Since(started), mapped)
		return shape.Value{}, mapped, 0
	}
	defer response.Body.Close()
	if date, err := http.ParseTime(response.Header.Get("Date")); err == nil {
		c.LastDate = date
	}

	if response.StatusCode < 200 || response.StatusCode > 299 {
		excerpt, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
		mapped := statusError(response, excerpt, request)
		c.logRequest(request, response.StatusCode, time.Since(started), mapped)
		return shape.Value{}, mapped, retryAfter(response)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		mapped := c.transportError(ctx, requestCtx, request, err)
		return shape.Value{}, mapped, 0
	}
	c.logRequest(request, response.StatusCode, time.Since(started), nil)
	value, parseErr := shape.Parse(content)
	if parseErr != nil {
		return shape.Value{}, errs.New(errs.Upstream, "Trello returned a response that is not JSON.").
			WithDetail("upstream_status", response.StatusCode), 0
	}
	return value, nil, 0
}

func (c *Client) build(ctx context.Context, request *Request, header string) (*http.Request, *errs.Error) {
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
	httpRequest, err := http.NewRequestWithContext(ctx, request.Method, c.URL(request), body)
	if err != nil {
		return nil, errs.New(errs.Internal, "The request could not be built.")
	}
	httpRequest.Header.Set("Authorization", header)
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
			return errs.New(errs.Timeout, "No response from Trello in time; the change may or may not have been made.").
				WithRetriable(false).WithDetail("write_state", "unknown").
				WithHint("Check the card in Trello before trying again.")
		}
		message := fmt.Sprintf("Trello did not respond within %s.", c.Timeout)
		if errors.Is(parent.Err(), context.DeadlineExceeded) {
			message = "The invocation's time budget ran out before Trello responded."
		}
		return errs.New(errs.Timeout, "%s", message)
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return errs.New(errs.Network, "Could not resolve %s.", host).WithHint(ToolName + " doctor")
	}
	var tlsErr *tls.CertificateVerificationError
	if errors.As(err, &tlsErr) {
		return errs.New(errs.Network, "TLS verification failed for %s.", host).WithHint(ToolName + " doctor")
	}
	if strings.Contains(err.Error(), "redirect") {
		return errs.New(errs.Upstream, "Trello redirected the request somewhere it cannot follow.").WithRetriable(false)
	}
	return errs.New(errs.Network, "Could not connect to %s.", host).WithHint(ToolName + " doctor")
}

// ToolName is used in hints.
const ToolName = "trello"

func statusError(response *http.Response, body []byte, request *Request) *errs.Error {
	status := response.StatusCode
	excerpt := strings.TrimSpace(string(body))
	if runes := []rune(excerpt); len(runes) > maxExcerptChars {
		excerpt = string(runes[:maxExcerptChars])
	}
	var err *errs.Error
	switch {
	case status == 401:
		err = errs.New(errs.Auth, "Trello rejected the credentials.").WithHint(ToolName + " doctor")
	case status == 403:
		err = errs.New(errs.Auth, "This token is not permitted to do that.").
			WithHint("Check the token's scope and that its member can see the board.")
	case status == 404:
		err = errs.New(errs.NotFound, "Trello has no such resource visible to this token.")
	case status == 400 || status == 422:
		err = errs.New(errs.Validation, "Trello rejected the request: %s.", firstLine(excerpt))
	case status == 409 || status == 412:
		err = errs.New(errs.Conflict, "Trello reported a conflict.")
	case status == 429:
		err = errs.New(errs.RateLimited, "Trello is rate limiting this token.")
		if wait := retryAfter(response); wait > 0 {
			err.WithRetryAfter(wait.Milliseconds())
		}
	case status == 408 || status == 504:
		err = errs.New(errs.Timeout, "Trello timed out (%d).", status)
		if request.IsWrite() {
			err.WithRetriable(false).WithDetail("write_state", "unknown")
		}
	case status == 405 || status == 501:
		err = errs.New(errs.Internal, "Trello does not support this request (%d).", status)
	default:
		err = errs.New(errs.Upstream, "Trello returned %d.", status)
	}
	err.WithDetail("upstream_status", status)
	if excerpt != "" {
		err.WithDetail("upstream_excerpt", excerpt)
	}
	return err
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	line = strings.TrimRight(line, ".")
	if line == "" {
		return "no reason given"
	}
	return line
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
	return parsed.String()
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
