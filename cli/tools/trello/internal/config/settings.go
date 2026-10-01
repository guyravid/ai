package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type Kind string

const (
	KindInt      Kind = "integer"
	KindDuration Kind = "duration"
	KindSize     Kind = "size"
	KindPath     Kind = "path"
	KindName     Kind = "name"
	KindLevel    Kind = "level"
	KindURL      Kind = "url"
)

type Origin string

const (
	OriginFlag        Origin = "flag"
	OriginEnvProfile  Origin = "env_profile"
	OriginEnvDefault  Origin = "env_default"
	OriginFileProfile Origin = "file_profile"
	OriginFileDefault Origin = "file_default"
	OriginFamily      Origin = "family"
	OriginBuiltin     Origin = "builtin"
)

// Def declares one setting. The same list drives resolution, list-config, the config schema, and
// `teach config`, so a setting added here appears in all four.
type Def struct {
	Name        string
	Kind        Kind
	Default     any // int64 or string; nil when the default is computed (CONFIG, DATASET_DIR)
	Flag        string
	Min, Max    int64 // for KindInt; Max 0 means unbounded
	InFile      bool  // may be set in the config file
	Description string
}

// FileKey is the member name used in the config file.
func (d *Def) FileKey() string { return strings.ToLower(d.Name) }

var logLevels = []string{"off", "error", "warn", "info", "debug"}

// Defs returns the contract's settings (§13.5) followed by this tool's own, in display order.
func Defs() []*Def {
	return []*Def{
		{Name: "CONFIG", Kind: KindPath, Flag: "--config", Description: "Path to the config file"},
		{Name: "PROFILE", Kind: KindName, Flag: "--profile", Description: "Active profile"},
		{Name: "LIMIT", Kind: KindInt, Default: int64(25), Flag: "--limit", Min: 1, InFile: true, Description: "Records returned per call; clamped to each command's maximum"},
		{Name: "MAX_BYTES", Kind: KindInt, Default: int64(32768), Flag: "--max-bytes", Min: 1, Max: 1048576, InFile: true, Description: "Size cap on the whole encoded response"},
		{Name: "MAX_STRING", Kind: KindInt, Default: int64(2048), Flag: "--max-string", Min: 1, Max: 65536, InFile: true, Description: "Characters kept per string value"},
		{Name: "MAX_DEPTH", Kind: KindInt, Default: int64(8), Flag: "--max-depth", Min: 1, Max: 32, InFile: true, Description: "Nesting depth kept in data"},
		{Name: "MAX_PAGES", Kind: KindInt, Default: int64(10), Flag: "--max-pages", Min: 1, Max: 100, InFile: true, Description: "Upstream pages fetched per invocation"},
		{Name: "TIMEOUT", Kind: KindDuration, Default: "30s", Flag: "--timeout", InFile: true, Description: "Deadline per upstream request"},
		{Name: "BUDGET", Kind: KindDuration, Default: "60s", Flag: "--budget", InFile: true, Description: "Deadline for the whole invocation; 600s with --all unless set"},
		{Name: "DATASET_DIR", Kind: KindPath, Flag: "--dataset-dir", InFile: true, Description: "Where --all datasets are stored"},
		{Name: "DATASET_TTL", Kind: KindDuration, Default: "1h", InFile: true, Description: "A dataset expires this long after it was last read"},
		{Name: "DATASET_MAX_BYTES", Kind: KindSize, Default: "256MB", InFile: true, Description: "Size cap on one dataset"},
		{Name: "DATASET_MAX_RECORDS", Kind: KindInt, Default: int64(1000000), Min: 1, InFile: true, Description: "Record cap on one dataset"},
		{Name: "DATASET_TOTAL_BYTES", Kind: KindSize, Default: "2GB", InFile: true, Description: "Size budget for the whole dataset directory"},
		{Name: "LOG_LEVEL", Kind: KindLevel, Default: "error", InFile: true, Description: "Diagnostics on stderr: off, error, warn, info, debug. --verbose sets debug"},
		{Name: "BASE_URL", Kind: KindURL, Default: "https://api.trello.com/1", InFile: true, Description: "Trello API root. Overrides are honoured only for loopback hosts"},
	}
}

// parseValue validates a raw textual value for a setting and returns its typed form: int64 for
// integers, string for everything else.
func parseValue(def *Def, raw string) (any, error) {
	switch def.Kind {
	case KindInt:
		number, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a whole number", raw)
		}
		if number < def.Min {
			return nil, fmt.Errorf("%d is below the minimum of %d", number, def.Min)
		}
		return number, nil
	case KindDuration:
		if _, err := ParseDuration(raw); err != nil {
			return nil, err
		}
		return raw, nil
	case KindSize:
		if _, err := ParseSize(raw); err != nil {
			return nil, err
		}
		return raw, nil
	case KindLevel:
		for _, level := range logLevels {
			if raw == level {
				return raw, nil
			}
		}
		return nil, fmt.Errorf("%q is not one of %s", raw, strings.Join(logLevels, ", "))
	case KindURL:
		parsed, err := url.Parse(raw)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
			return nil, fmt.Errorf("%q is not an http(s) URL", raw)
		}
		return strings.TrimRight(raw, "/"), nil
	case KindName:
		if err := ValidateProfileName(raw); err != nil {
			return nil, err
		}
		return raw, nil
	default:
		if raw == "" {
			return nil, fmt.Errorf("the path is empty")
		}
		return raw, nil
	}
}

// parseFileValue converts a config-file member into the textual form parseValue accepts.
// Integers must be JSON numbers; everything else must be a JSON string.
func parseFileValue(def *Def, raw json.RawMessage) (any, error) {
	if def.Kind == KindInt {
		var number json.Number
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		if err := decoder.Decode(&number); err != nil {
			return nil, fmt.Errorf("must be a whole number")
		}
		return parseValue(def, number.String())
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		if def.Kind == KindDuration || def.Kind == KindSize {
			return nil, fmt.Errorf("must be a string with units, such as \"30s\" or \"256MB\"; a bare number is not accepted")
		}
		return nil, fmt.Errorf("must be a string")
	}
	return parseValue(def, text)
}
