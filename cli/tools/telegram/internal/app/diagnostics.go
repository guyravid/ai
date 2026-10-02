package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/guyravid/ai/cli/tools/telegram/internal/config"
	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
	"github.com/guyravid/ai/cli/tools/telegram/internal/secrets"
	"github.com/guyravid/ai/cli/tools/telegram/internal/shape"
	"github.com/guyravid/ai/cli/tools/telegram/internal/upstream"
)

const (
	checkTimeout = 5 * time.Second
	maxClockSkew = 60 * time.Second
	statusPass   = "pass"
	statusWarn   = "warn"
	statusFail   = "fail"
	statusSkip   = "skip"
	// identityPath is the Bot API method that returns the bot itself: the cheapest authenticated read.
	identityPath = "/getMe"
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
		if s.tokenErr != nil {
			// Nothing scoped to the profile resolved, but a default-scope source exists and is skipped.
			credentialsCheck = check{Name: "credentials", Status: statusFail, Detail: s.tokenErr.Message +
				" It would come from " + s.tokenErr.Details["would_use_source"].(string) + ".", err: s.tokenErr}
		}
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
	var networkCheck check
	if s.tokenErr != nil {
		networkCheck = check{Name: "network", Status: statusSkip, Detail: "Skipped: this profile has no bot token of its own."}
	} else {
		networkCheck = s.networkCheck(ctx, baseURL)
	}
	add(networkCheck)

	authCheck := check{Name: "auth", Status: statusSkip}
	clockCheck := check{Name: "clock", Status: statusSkip, Detail: "Skipped: needs an authenticated response."}
	switch {
	case credentialsCheck.Status == statusFail || credentialsCheck.Status == statusSkip:
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
	} else {
		add(s.datasetsCheck())
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

// authAndClock makes the one authenticated read: getMe, which also reports the bot's identity.
func (s *session) authAndClock(ctx context.Context) (check, check) {
	clockSkip := check{Name: "clock", Status: statusSkip, Detail: "Skipped: needs an authenticated response."}
	if err := s.newClient(); err != nil {
		return check{Name: "auth", Status: statusFail, Detail: err.Message, err: err}, clockSkip
	}
	s.client.Timeout = checkTimeout
	ctx, cancel := context.WithTimeout(ctx, 2*checkTimeout)
	defer cancel()
	identity, err := s.client.Do(ctx, &upstream.Request{Method: "POST", Path: identityPath})
	if err != nil {
		mapped := errs.From(err)
		return check{Name: "auth", Status: statusFail, Detail: mapped.Message, err: mapped}, clockSkip
	}
	detail := "Authenticated."
	username, hasUsername := identity.Get("username")
	id, hasID := identity.Get("id")
	switch {
	case hasUsername && username.IsString() && hasID:
		detail = fmt.Sprintf("Authenticated as bot @%s (id %s).", username.Text(), string(id.Raw))
	case hasUsername && username.IsString():
		detail = fmt.Sprintf("Authenticated as bot @%s.", username.Text())
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

// datasetsCheck confirms the dataset directory exists or can be created, and is writable (contract
// §7.1). This tool stores no datasets, but the check and the DATASET_DIR setting are part of every
// tool, and a misconfigured directory is still worth reporting.
func (s *session) datasetsCheck() check {
	dir := s.settings.String("DATASET_DIR")
	if dir == "" {
		reason := "it cannot be determined"
		if s.settings.DatasetDirErr != nil {
			reason = s.settings.DatasetDirErr.Error()
		}
		failure := errs.Configf("The dataset directory is not set and %s.", reason).
			WithHint(fmt.Sprintf("Pass --dataset-dir <path> or set %s_DATASET_DIR.", s.app.Build.Prefix))
		return check{Name: "datasets", Status: statusFail, Detail: failure.Message, err: failure}
	}
	if reason := probeDirectory(dir); reason != "" {
		failure := errs.Configf("The dataset directory %s cannot be created or written.", dir).
			WithHint(fmt.Sprintf("Set --dataset-dir or %s_DATASET_DIR to a writable directory.", s.app.Build.Prefix)).
			WithDetail("dataset_dir", dir).WithDetail("reason", reason)
		return check{Name: "datasets", Status: statusFail, Detail: failure.Message, err: failure}
	}
	return check{Name: "datasets", Status: statusPass, Detail: dir + " is writable."}
}

// probeDirectory creates dir owner-only if needed and writes and removes a probe file in it. It
// returns the reason it is unusable, or "".
func probeDirectory(dir string) string {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return rootCause(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return rootCause(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return rootCause(err)
	}
	defer root.Close()
	probe := fmt.Sprintf(".probe-%d", os.Getpid())
	file, err := root.OpenFile(probe, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return rootCause(err)
	}
	file.Close()
	_ = root.Remove(probe)
	return ""
}

func rootCause(err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err.Error()
	}
	return err.Error()
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
		if s.tokenErr != nil && credential.Name == "BOT_TOKEN" {
			// Not set for this profile: the default-scope source that exists is skipped, and named here.
			entry.Hint = "Profile '" + s.settings.Profile + "' skips the default bot's token (" +
				fmt.Sprint(s.tokenErr.Details["would_use_source"]) + "). " + s.tokenErr.Hint + "."
		} else if !set {
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
		case config.KindChat:
			property["type"], property["pattern"] = "string", `^-?[0-9]{1,20}$|^@[A-Za-z0-9_]{5,32}$`
		case config.KindChatList:
			property["type"], property["pattern"] = "string", `^(-?[0-9]{1,20}|@[A-Za-z0-9_]{5,32})(\s*,\s*(-?[0-9]{1,20}|@[A-Za-z0-9_]{5,32}))*$|^$`
		case config.KindEnum:
			property["type"], property["enum"] = "string", def.Enum
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
