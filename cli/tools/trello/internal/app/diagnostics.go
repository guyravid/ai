package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/guyravid/ai/cli/tools/trello/internal/config"
	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
	"github.com/guyravid/ai/cli/tools/trello/internal/secrets"
	"github.com/guyravid/ai/cli/tools/trello/internal/shape"
	"github.com/guyravid/ai/cli/tools/trello/internal/upstream"
)

const (
	checkTimeout = 5 * time.Second
	maxClockSkew = 60 * time.Second
	statusPass   = "pass"
	statusWarn   = "warn"
	statusFail   = "fail"
	statusSkip   = "skip"
	identityPath = "/members/me"
)

type check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	err    *errs.Error
}

type doctorData struct {
	Profile *string `json:"profile"`
	Checks  []check `json:"checks"`
}

// doctor runs every required check in order, reporting a check that cannot run as skip, never
// omitting it (contract §7.1). It never changes upstream state.
func (s *session) doctor(ctx context.Context) *Response {
	var checks []check
	add := func(result check) { checks = append(checks, result) }

	loadErr := s.load()
	configCheck := check{Name: "config", Status: statusPass}
	credentialsCheck := check{Name: "credentials"}
	switch {
	case loadErr == nil:
		configCheck.Detail = s.configDetail()
		credentialsCheck = s.credentialsCheck()
	case s.settings == nil:
		configCheck = check{Name: "config", Status: statusFail, Detail: loadErr.Message, err: loadErr}
		credentialsCheck = check{Name: "credentials", Status: statusSkip, Detail: "Skipped: the configuration did not load."}
	default:
		// Settings loaded; a credential source is unusable.
		configCheck.Detail = s.configDetail()
		credentialsCheck = check{Name: "credentials", Status: statusFail, Detail: loadErr.Message, err: loadErr}
	}
	add(configCheck)
	add(credentialsCheck)

	baseURL := defaultBaseURL
	if s.settings != nil {
		baseURL = s.settings.String("BASE_URL")
	}
	networkCheck := s.networkCheck(ctx, baseURL)
	add(networkCheck)

	authCheck := check{Name: "auth", Status: statusSkip}
	clockCheck := check{Name: "clock", Status: statusSkip, Detail: "Skipped: needs an authenticated response."}
	switch {
	case credentialsCheck.Status != statusPass:
		authCheck.Detail = "Skipped: credentials incomplete."
	case networkCheck.Status != statusPass:
		authCheck.Detail = "Skipped: upstream unreachable."
	default:
		authCheck, clockCheck = s.authAndClock(ctx)
	}
	add(authCheck)
	add(clockCheck)

	if s.settings == nil {
		add(check{Name: "datasets", Status: statusSkip, Detail: "Skipped: the configuration did not load."})
	} else if store, err := s.openStore(); err != nil {
		add(check{Name: "datasets", Status: statusFail, Detail: err.Message, err: err})
	} else {
		sidecars := store.List()
		var total int64
		for _, sidecar := range sidecars {
			total += sidecar.Bytes
		}
		add(check{Name: "datasets", Status: statusPass,
			Detail: fmt.Sprintf("%s is writable; %d datasets, %s.", store.Dir, len(sidecars), humanBytes(total))})
	}

	writes := "Writes enabled in this build."
	if !s.app.Build.WritesEnabled {
		writes = "This build is read-only."
	}
	add(check{Name: "writes", Status: statusPass, Detail: writes})

	var profile *string
	if s.settings != nil && s.settings.Profile != "" {
		profile = &s.settings.Profile
	}
	data := mustValue(doctorData{Profile: profile, Checks: checks})
	for _, result := range checks {
		if result.Status == statusFail {
			s.envelope.SetError(result.err)
			break
		}
	}
	s.envelope.Data = data
	return s.finish(Encode(s.envelope, s.invocation.Bool("pretty")))
}

func (s *session) configDetail() string {
	detail := "No config file; using environment and defaults."
	if s.settings.File != nil {
		detail = "Loaded " + s.settings.File.Path + "."
	}
	if s.settings.Profile != "" {
		detail += fmt.Sprintf(" Profile '%s' exists.", s.settings.Profile)
	}
	return detail
}

func (s *session) credentialsCheck() check {
	var parts, missing []string
	var failure *errs.Error
	for _, credential := range s.creds {
		if !credential.Set {
			missing = append(missing, credential.Name)
			if failure == nil {
				failure = errs.New(errs.Auth, "A required credential is missing.").
					WithHint(secrets.MissingHint(s.app.Build.Prefix, credential.Name))
			}
			continue
		}
		part := fmt.Sprintf("%s from %s", credential.Name, credential.Source)
		if credential.File != nil {
			part += fmt.Sprintf(" (%s, mode %s)", credential.File.Path, credential.File.Mode)
		}
		parts = append(parts, part)
	}
	if failure != nil {
		detail := strings.Join(missing, " and ") + " not set by any source."
		if len(parts) > 0 {
			detail += " " + strings.Join(parts, ". ") + "."
		}
		return check{Name: "credentials", Status: statusFail, Detail: detail, err: failure}
	}
	status := statusPass
	for _, credential := range s.creds {
		if credential.File != nil && credential.File.Mode == "unverified" {
			status = statusWarn
		}
	}
	return check{Name: "credentials", Status: status, Detail: strings.Join(parts, ". ") + "."}
}

// networkCheck resolves the host and completes a TLS 1.2+ handshake.
func (s *session) networkCheck(ctx context.Context, baseURL string) check {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return check{Name: "network", Status: statusFail, Detail: "BASE_URL is not a URL.",
			err: errs.Configf("BASE_URL is not a URL.")}
	}
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		port = "443"
		if parsed.Scheme == "http" {
			port = "80"
		}
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	fail := func(detail string) check {
		return check{Name: "network", Status: statusFail, Detail: detail,
			err: errs.New(errs.Network, "%s", detail).WithHint("Check DNS, proxy, and firewall settings for " + host + ".")}
	}
	if _, err := net.DefaultResolver.LookupHost(ctx, host); err != nil {
		return fail("Could not resolve " + host + ".")
	}
	dialer := &net.Dialer{}
	if parsed.Scheme == "http" {
		connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
		if err != nil {
			return fail("Could not connect to " + host + ".")
		}
		connection.Close()
		return check{Name: "network", Status: statusPass, Detail: host + " reachable over plain HTTP (loopback)."}
	}
	tlsDialer := &tls.Dialer{NetDialer: dialer, Config: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}}
	connection, err := tlsDialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return fail("Could not complete a TLS handshake with " + host + ".")
	}
	defer connection.Close()
	version := "TLS"
	if state, ok := connection.(*tls.Conn); ok {
		version = tls.VersionName(state.ConnectionState().Version)
	}
	return check{Name: "network", Status: statusPass, Detail: fmt.Sprintf("%s resolved; %s.", host, version)}
}

// authAndClock makes the one authenticated read: GET /members/me.
func (s *session) authAndClock(ctx context.Context) (check, check) {
	clockSkip := check{Name: "clock", Status: statusSkip, Detail: "Skipped: needs an authenticated response."}
	if err := s.newClient(); err != nil {
		return check{Name: "auth", Status: statusFail, Detail: err.Message, err: err}, clockSkip
	}
	s.client.Timeout = checkTimeout
	ctx, cancel := context.WithTimeout(ctx, 2*checkTimeout)
	defer cancel()
	identity, err := s.client.Do(ctx, &upstream.Request{Method: "GET", Path: identityPath,
		Query: url.Values{"fields": {"username,fullName"}}})
	if err != nil {
		mapped := errs.From(err)
		return check{Name: "auth", Status: statusFail, Detail: mapped.Message, err: mapped}, clockSkip
	}
	detail := "Authenticated."
	if username, ok := identity.Get("username"); ok && username.IsString() {
		detail = fmt.Sprintf("Authenticated as member '%s'.", username.Text())
	}
	authCheck := check{Name: "auth", Status: statusPass, Detail: detail}
	if s.client.LastDate.IsZero() {
		return authCheck, check{Name: "clock", Status: statusSkip, Detail: "Skipped: upstream sent no Date header."}
	}
	skew := s.app.Now().Sub(s.client.LastDate)
	if skew < 0 {
		skew = -skew
	}
	if skew > maxClockSkew {
		return authCheck, check{Name: "clock", Status: statusWarn,
			Detail: fmt.Sprintf("Local time differs from upstream by %s.", skew.Round(time.Second))}
	}
	return authCheck, check{Name: "clock", Status: statusPass, Detail: fmt.Sprintf("Within %s of upstream.", max(skew.Round(time.Second), time.Second))}
}

func humanBytes(size int64) string {
	switch {
	case size >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(size)/(1<<30))
	case size >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(size)/(1<<20))
	case size >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(size)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", size)
}

type settingEntry struct {
	Name    string `json:"name"`
	Value   any    `json:"value"`
	Set     *bool  `json:"set,omitempty"`
	Source  string `json:"source"`
	Origin  string `json:"origin"`
	Default any    `json:"default"`
	Hint    string `json:"hint,omitempty"`
}

type listConfigData struct {
	Profile    *string        `json:"profile"`
	ConfigFile *string        `json:"config_file"`
	Settings   []settingEntry `json:"settings"`
}

// listConfig reports every setting and where it came from (contract §7.2). It never contacts
// upstream.
func (s *session) listConfig() *Response {
	if s.invocation.Bool("schema") {
		s.envelope.Data = shape.NewObject(shape.Field{Key: "schema", Value: mustValue(configSchema())})
		return s.finish(Encode(s.envelope, s.invocation.Bool("pretty")))
	}
	var entries []settingEntry
	for _, setting := range s.settings.Settings {
		entries = append(entries, settingEntry{Name: setting.Def.Name, Value: setting.Value, Source: setting.Source,
			Origin: string(setting.Origin), Default: setting.Default})
	}
	for _, credential := range s.creds {
		set := credential.Set
		entry := settingEntry{Name: credential.Name, Value: nil, Set: &set, Source: credential.Source,
			Origin: string(credential.Origin), Default: nil}
		if !set {
			entry.Hint = secrets.MissingHint(s.app.Build.Prefix, credential.Name)
		}
		entries = append(entries, entry)
	}
	data := listConfigData{Settings: entries}
	if s.settings.Profile != "" {
		data.Profile = &s.settings.Profile
	}
	if s.settings.File != nil {
		data.ConfigFile = &s.settings.File.Path
	}
	s.envelope.Data = mustValue(data)
	return s.finish(Encode(s.envelope, s.invocation.Bool("pretty")))
}

// configSchema is the JSON Schema of the config file. It has no member that can hold a credential
// value: only <name>_file paths and env_file.
func configSchema() map[string]any {
	properties := map[string]any{}
	for _, def := range config.Defs() {
		if !def.InFile {
			continue
		}
		property := map[string]any{"description": def.Description}
		switch def.Kind {
		case config.KindInt:
			property["type"] = "integer"
			property["minimum"] = def.Min
			if def.Max > 0 {
				property["maximum"] = def.Max
			}
		case config.KindDuration:
			property["type"], property["pattern"] = "string", `^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$|^[0-9]+d$`
		case config.KindSize:
			property["type"], property["pattern"] = "string", `^[0-9]+(B|KB|MB|GB|TB|KiB|MiB|GiB|TiB)$`
		case config.KindLevel:
			property["enum"] = []string{"off", "error", "warn", "info", "debug"}
		case config.KindURL:
			property["type"], property["format"] = "string", "uri"
		default:
			property["type"] = "string"
		}
		properties[def.FileKey()] = property
	}
	for _, name := range Credentials {
		properties[strings.ToLower(name)+"_file"] = map[string]any{"type": "string",
			"description": "Path to a file holding " + name + ". Never the value itself."}
	}
	properties["env_file"] = map[string]any{"type": "string", "description": "Path to a KEY=value file of credentials."}
	// Not a setting (contract §13.4 rule 6): read only by list-profiles. "Not empty after trimming"
	// is checked by the loader; the schema cannot express it.
	properties[config.DescriptionKey] = map[string]any{"type": "string", "minLength": 1, "maxLength": config.MaxDescriptionLength,
		"pattern":     `^[^\r\n]*$`,
		"description": "One line saying what this profile is for. Shown by list-profiles; not a setting."}
	return map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"config_version": map[string]any{"const": 1},
			"default":        map[string]any{"$ref": "#/$defs/settings"},
			"profiles": map[string]any{"type": "object",
				"propertyNames":        map[string]any{"pattern": "^[A-Za-z0-9][A-Za-z0-9_-]*$", "not": map[string]any{"enum": []string{"FILE", "file"}}},
				"additionalProperties": map[string]any{"$ref": "#/$defs/settings"}},
		},
		"$defs": map[string]any{"settings": map[string]any{"type": "object", "additionalProperties": false, "properties": properties}},
	}
}

// listProfiles reports the profiles the tool can run with (contract §7.3). Names only, never values.
func (s *session) listProfiles() *Response {
	var environ []string
	if s.app.Environ != nil {
		environ = s.app.Environ()
	}
	list, err := config.ListProfiles(config.Input{
		Tool: s.app.Build.Tool, Prefix: s.app.Build.Prefix, Flags: s.invocation.Flags, Env: s.app.Env,
		GOOS: s.app.Build.GOOS, Credentials: Credentials,
	}, environ)
	if err != nil {
		return s.fail(err)
	}
	if list.Undeclared != "" {
		warning := config.UndeclaredWarning(s.app.Build.Prefix, list.Undeclared)
		s.envelope.AddWarning(warning.Code, warning.Message)
	}
	return s.discovery(mustValue(list.Entries))
}
