// Package app is the single response path: it parses a call, runs it, shapes the result, and
// produces the one document written to stdout (base template §4). Nothing else writes to stdout.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/guyravid/ai/cli/tools/trello/internal/config"
	"github.com/guyravid/ai/cli/tools/trello/internal/datasets"
	"github.com/guyravid/ai/cli/tools/trello/internal/envelope"
	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
	"github.com/guyravid/ai/cli/tools/trello/internal/registry"
	"github.com/guyravid/ai/cli/tools/trello/internal/secrets"
	"github.com/guyravid/ai/cli/tools/trello/internal/shape"
	"github.com/guyravid/ai/cli/tools/trello/internal/upstream"
)

const (
	defaultBaseURL = "https://api.trello.com/1"
	allBudget      = 600 * time.Second
	allMaxPages    = 1000
)

// Credentials this tool declares (contract §12.1).
var Credentials = []string{"API_KEY", "API_TOKEN"}

type Build struct {
	Tool          string
	Prefix        string
	Version       string
	Commit        string
	WritesEnabled bool
	MCPEnabled    bool
	GOOS          string
	GOARCH        string
}

// ServeFunc runs the MCP server. It is nil in builds without server mode.
type ServeFunc func(ctx context.Context, app *App, invocation *Invocation) *Response

type App struct {
	Build    Build
	Registry *registry.Registry
	Env      config.Env
	// Environ lists the whole environment as KEY=value pairs. Only list-profiles needs it: profiles
	// declared by variables can be found only by enumerating them. nil means none.
	Environ   func() []string
	Now       func() time.Time
	Stderr    io.Writer
	Transport http.RoundTripper // injected by tests; nil uses the default transport
	Serve     ServeFunc
	// StartupError is a registry validation failure; every call then reports internal.
	StartupError error
	// MutatingNames are the tool's mutating commands, known even to a read-only build.
	MutatingNames []string
}

// Response is the outcome of one call. Encoded holds the final, redacted bytes for stdout.
type Response struct {
	Envelope *envelope.Envelope
	Encoded  []byte
	Text     string // Markdown or help text, for teach and --help
	Exit     int
	// Quiet means nothing goes to stdout: serve owned it, and a stdio client may still be reading.
	Quiet    bool
	redactor *secrets.Redactor
	cleanup  func()
}

// Document is the redacted output a caller sees: the Markdown or help text, else the envelope.
func (r *Response) Document() []byte {
	if r.Text != "" {
		return r.redactor.Apply([]byte(r.Text))
	}
	return r.Encoded
}

// Cleanup runs deferred housekeeping after the response has been written.
func (r *Response) Cleanup() {
	if r.cleanup != nil {
		r.cleanup()
	}
}

// session holds what one call resolves: settings, credentials, redactor, and upstream client.
type session struct {
	app        *App
	invocation *Invocation
	envelope   *envelope.Envelope
	settings   *config.Resolved
	creds      []*secrets.Credential
	redactor   *secrets.Redactor
	client     *upstream.Client
	now        time.Time
	started    time.Time
	store      *datasets.Store
	log        *logger
}

// Execute runs one invocation through the whole pipeline and returns the finished response.
func (a *App) Execute(ctx context.Context, invocation *Invocation) (response *Response) {
	name := invocation.Command.Name
	s := &session{
		app:        a,
		invocation: invocation,
		envelope:   envelope.New(a.Build.Tool, a.Build.Version, &name),
		now:        a.Now(),
		started:    time.Now(),
		redactor:   secrets.NewRedactor(nil, nil),
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			s.envelope = envelope.New(a.Build.Tool, a.Build.Version, &name)
			response = s.fail(errs.New(errs.Internal, "The tool failed unexpectedly: %v.", recovered).
				WithHint("Report this with --verbose output."))
		}
		if s.store != nil {
			store := s.store
			previous := response.cleanup
			response.cleanup = func() {
				if previous != nil {
					previous()
				}
				if err := store.Cleanup(); err != nil && s.log != nil {
					s.log.write("warn", "dataset cleanup failed", map[string]any{"code": "cleanup_failed"})
				}
				store.Close()
			}
		}
	}()
	if invocation.Bool("dry-run") {
		s.envelope.Meta.DryRun = true
	}

	switch name {
	case "tools":
		return s.discovery(a.tools(invocation))
	case "describe":
		value, err := a.describe(invocation)
		if err != nil {
			return s.fail(err)
		}
		return s.discovery(value)
	case "version":
		return s.discovery(a.version())
	case "teach":
		return a.teach(invocation)
	case "serve":
		if a.Serve == nil {
			return s.fail(errs.New(errs.Refused, "This build does not include MCP server mode.").
				WithHint("Use a build made with -tags mcp.").WithDetail("reason", "mcp_disabled"))
		}
		return a.Serve(ctx, a, invocation)
	case "doctor":
		return s.doctor(ctx)
	}

	// list-profiles resolves on its own: an undeclared selected profile, which makes load fail, is
	// exactly when the list is needed (contract §7.3).
	if name == "list-profiles" {
		return s.listProfiles()
	}
	if err := s.load(); err != nil {
		return s.fail(err)
	}
	switch {
	case name == "list-config":
		return s.listConfig()
	case strings.HasPrefix(name, "dataset."):
		return s.dataset()
	}
	return s.domain(ctx)
}

// load resolves settings and credentials and builds the redactor before any request is sent.
func (s *session) load() *errs.Error {
	flags := map[string]string{}
	for key, value := range s.invocation.Flags {
		flags[key] = value
	}
	settings, err := config.Load(config.Input{
		Tool: s.app.Build.Tool, Prefix: s.app.Build.Prefix, Flags: flags, Env: s.app.Env,
		GOOS: s.app.Build.GOOS, Credentials: Credentials,
	})
	if err != nil {
		return err
	}
	s.settings = settings
	for _, warning := range settings.Warnings {
		s.envelope.AddWarning(warning.Code, warning.Message)
	}
	creds, warnings, credErr := secrets.Resolve(secrets.Input{
		Prefix: s.app.Build.Prefix, Profile: settings.Profile, Env: s.app.Env, GOOS: s.app.Build.GOOS,
		File: settings.File, Names: Credentials,
	})
	if credErr != nil {
		return credErr
	}
	s.creds = creds
	for _, warning := range warnings {
		s.envelope.AddWarning(warning.Code, warning.Message)
	}
	var values, literals []string
	for _, credential := range creds {
		if credential.Set {
			values = append(values, credential.Secret.Reveal())
		}
	}
	if header, err := s.authHeader(); err == nil {
		literals = append(literals, header)
	}
	s.redactor = secrets.NewRedactor(values, literals)
	level := settings.String("LOG_LEVEL")
	if s.invocation.Bool("verbose") {
		level = "debug"
	}
	s.log = newLogger(s.app.Stderr, level, s.redactor)
	return nil
}

func (s *session) credential(name string) *secrets.Credential {
	for _, credential := range s.creds {
		if credential.Name == name {
			return credential
		}
	}
	return nil
}

// authHeader builds Trello's OAuth-style header. Credentials travel only here, never in URLs.
func (s *session) authHeader() (string, *errs.Error) {
	key, token := s.credential("API_KEY"), s.credential("API_TOKEN")
	for _, credential := range []*secrets.Credential{key, token} {
		if credential == nil || !credential.Set {
			name := "API_KEY"
			if credential != nil {
				name = credential.Name
			}
			return "", errs.New(errs.Auth, "No %s is configured.", strings.ReplaceAll(strings.ToLower(name), "_", " ")).
				WithHint(secrets.MissingHint(s.app.Build.Prefix, name))
		}
	}
	return fmt.Sprintf(`OAuth oauth_consumer_key="%s", oauth_token="%s"`, key.Secret.Reveal(), token.Secret.Reveal()), nil
}

func (s *session) newClient() *errs.Error {
	client, err := upstream.NewClient(s.settings.String("BASE_URL"), defaultBaseURL)
	if err != nil {
		return err
	}
	client.Credentials = s.authHeader
	client.Timeout = s.settings.Duration("TIMEOUT")
	client.Deterministic = s.invocation.Bool("deterministic")
	client.UserAgent = fmt.Sprintf("%s/%s agentcli/%s (%s/%s)", s.app.Build.Tool, s.app.Build.Version,
		envelope.ContractVersion, s.app.Build.GOOS, s.app.Build.GOARCH)
	if s.app.Transport != nil {
		client.HTTP.Transport = s.app.Transport
	}
	if s.log != nil {
		client.Log = s.log.write
	}
	s.client = client
	return nil
}

// fail turns the envelope into an error and finishes it.
func (s *session) fail(err *errs.Error) *Response {
	s.envelope.SetError(err)
	return s.finish(Encode(s.envelope, s.invocation.Bool("pretty")))
}

// Encode is envelope.Encode; kept here so every response goes through one function.
func Encode(e *envelope.Envelope, pretty bool) []byte { return envelope.Encode(e, pretty) }

// finish applies timing and the redactor to the encoded envelope.
func (s *session) finish(encoded []byte) *Response {
	if s.invocation.Bool("timing") && s.envelope.Meta.Timing == nil {
		timing := &envelope.Timing{TotalMs: time.Since(s.started).Milliseconds()}
		if s.client != nil {
			timing.UpstreamRequests = s.client.Requests
			timing.UpstreamMs = s.client.Elapsed.Milliseconds()
		}
		s.envelope.Meta.Timing = timing
		encoded = Encode(s.envelope, s.invocation.Bool("pretty"))
	}
	return &Response{
		Envelope: s.envelope,
		Encoded:  s.redactor.Apply(encoded),
		Exit:     s.envelope.ExitCode(),
		redactor: s.redactor,
	}
}

// discovery output is never capped or truncated (contract §6).
func (s *session) discovery(data shape.Value) *Response {
	s.envelope.Data = data
	return s.finish(Encode(s.envelope, s.invocation.Bool("pretty")))
}

func (s *session) maxBytes() int  { return int(s.settings.Int("MAX_BYTES")) }
func (s *session) maxString() int { return int(s.settings.Int("MAX_STRING")) }
func (s *session) maxDepth() int  { return int(s.settings.Int("MAX_DEPTH")) }

// shapeRecord projects, strips, and caps one record.
func (s *session) shapeRecord(record shape.Value, fields []string, path string) (shape.Value, []string) {
	shaped := s.projectAndStrip(record, fields)
	return shape.Cap(shaped, s.maxString(), s.maxDepth(), path)
}

func (s *session) projectAndStrip(record shape.Value, fields []string) shape.Value {
	if fields != nil {
		record = shape.Project(record, fields)
	}
	if !s.invocation.Bool("keep-empty") {
		record = shape.StripEmpty(record)
	}
	return record
}

func (s *session) markShaping(fields []string) {
	s.envelope.Meta.Fields = fields
	s.envelope.Meta.StrippedEmpty = !s.invocation.Bool("keep-empty")
}

// respondObject finishes a single-object response.
func (s *session) respondObject(value shape.Value, fields []string) *Response {
	shaped, elided := s.shapeRecord(value, fields, "")
	if fields != nil {
		s.markShaping(fields)
	} else {
		s.envelope.Meta.StrippedEmpty = !s.invocation.Bool("keep-empty")
	}
	s.envelope.Meta.ElidedFields = append(s.envelope.Meta.ElidedFields, elided...)
	return s.finish(envelope.FitObject(s.envelope, shaped, s.maxBytes(), s.invocation.Bool("pretty")))
}

// respondList shapes records and fits them under the byte cap. cursorAt builds the cursor that
// resumes at a record index in this response.
func (s *session) respondList(records []shape.Value, fields []string, page envelope.Page, cursorAt envelope.CursorAt) *Response {
	shaped := make([]shape.Value, len(records))
	for index, record := range records {
		var elided []string
		shaped[index], elided = s.shapeRecord(record, fields, fmt.Sprintf("[%d]", index))
		s.envelope.Meta.ElidedFields = append(s.envelope.Meta.ElidedFields, elided...)
	}
	s.markShaping(fields)
	page.Count = len(shaped)
	s.envelope.Meta.Page = &page
	encoded := envelope.FitList(s.envelope, shaped, s.maxBytes(), s.invocation.Bool("pretty"), cursorAt)
	if s.pruneElided(s.envelope.Meta.Page.Count) {
		// Removing paths only shrinks the document, so it still fits.
		encoded = Encode(s.envelope, s.invocation.Bool("pretty"))
	}
	return s.finish(encoded)
}

// pruneElided drops elision paths of records the byte cap removed, reporting whether any were.
func (s *session) pruneElided(kept int) bool {
	var pruned []string
	for _, path := range s.envelope.Meta.ElidedFields {
		var index int
		if _, err := fmt.Sscanf(path, "[%d]", &index); err == nil && index >= kept {
			continue
		}
		pruned = append(pruned, path)
	}
	changed := len(pruned) != len(s.envelope.Meta.ElidedFields)
	s.envelope.Meta.ElidedFields = pruned
	return changed
}

// resolveLimit applies the command's default and maximum (contract §8.1).
func (s *session) resolveLimit(limits *registry.Limits, fromCursor string) int {
	limit := int(s.settings.Int("LIMIT"))
	setting := s.settings.Get("LIMIT")
	if setting.Origin == config.OriginBuiltin {
		limit = limits.Default
		if fromCursor != "" {
			fmt.Sscanf(fromCursor, "%d", &limit)
		}
	}
	if limit > limits.Max {
		s.envelope.AddWarning("limit_clamped", fmt.Sprintf("--limit %d exceeds the maximum of %d for this command and was reduced.", limit, limits.Max))
		limit = limits.Max
	}
	return max(limit, 1)
}

// errDryRun stops a read before it is sent, carrying the request it would have made.
type errDryRun struct{ request *upstream.Request }

func (errDryRun) Error() string { return "dry run" }

type dryRunDoer struct{}

func (dryRunDoer) Do(_ context.Context, request *upstream.Request) (shape.Value, error) {
	return shape.Value{}, errDryRun{request: request}
}

func (s *session) doer() registry.Doer {
	if s.invocation.Bool("dry-run") {
		return dryRunDoer{}
	}
	return s.client
}

// asDryRun converts a captured request into the dry-run response.
func (s *session) asDryRun(err error) (*Response, bool) {
	var captured errDryRun
	if !errors.As(err, &captured) {
		return nil, false
	}
	s.envelope.Data = shape.NewObject(shape.Field{Key: "preview", Value: s.client.Preview(captured.request)})
	return s.finish(Encode(s.envelope, s.invocation.Bool("pretty"))), true
}

// commandLine reconstructs the call as a command line, for hints.
func commandLine(tool string, invocation *Invocation, extra ...string) string {
	parts := append([]string{tool}, invocation.Command.Argv()...)
	for _, name := range invocation.Command.Positional() {
		if value, ok := invocation.Params[name]; ok {
			parts = append(parts, quote(fmt.Sprint(value)))
		}
	}
	for _, param := range invocation.Command.Params {
		if param.Positional {
			continue
		}
		if value, ok := invocation.Params[param.Name]; ok {
			parts = append(parts, param.Flag, quote(fmt.Sprint(value)))
		}
	}
	return strings.Join(append(parts, extra...), " ")
}

func quote(value string) string {
	if value == "" || strings.ContainsAny(value, " \t\"'$`\\;&|<>*?()[]{}") {
		encoded, _ := json.Marshal(value)
		return string(encoded)
	}
	return value
}
