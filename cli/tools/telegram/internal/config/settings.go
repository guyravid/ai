package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
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
	KindChat     Kind = "chat"      // one Telegram chat id or @channel username
	KindChatList Kind = "chat list" // comma-separated chat ids
	KindEnum     Kind = "enum"      // one of Def.Enum
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
	Min, Max    int64         // for KindInt; Max 0 means unbounded
	MinDuration time.Duration // for KindDuration; 0 means no minimum
	Enum        []string      // for KindEnum
	InFile      bool          // may be set in the config file
	Description string
}

// FileKey is the member name used in the config file.
func (d *Def) FileKey() string { return strings.ToLower(d.Name) }

var logLevels = []string{"off", "error", "warn", "info", "debug"}

// ParseModes are the values of PARSE_MODE.
var ParseModes = []string{"none", "html", "markdownv2"}

// MinPollInterval keeps a wait from busy-looping against the Bot API (plan trap 6).
const MinPollInterval = 500 * time.Millisecond

var (
	numericChatPattern  = regexp.MustCompile(`^-?[0-9]{1,20}$`)
	usernameChatPattern = regexp.MustCompile(`^@[A-Za-z0-9_]{5,32}$`)
)

// ValidChatID reports whether text is a chat id (possibly negative) or a public channel @username.
// Ids stay strings: channel ids can exceed 2^53.
func ValidChatID(text string) bool {
	return numericChatPattern.MatchString(text) || usernameChatPattern.MatchString(text)
}

// SplitChats splits a comma-separated chat list into trimmed ids. An empty text is an empty list.
func SplitChats(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	parts := strings.Split(text, ",")
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	return parts
}

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
		{Name: "BUDGET", Kind: KindDuration, Default: "60s", Flag: "--budget", InFile: true, Description: "Deadline for the whole invocation"},
		{Name: "DATASET_DIR", Kind: KindPath, Flag: "--dataset-dir", InFile: true, Description: "Dataset directory. Part of the contract; this tool stores no datasets"},
		{Name: "DATASET_TTL", Kind: KindDuration, Default: "1h", InFile: true, Description: "Dataset lifetime after the last read. Part of the contract; unused by this tool"},
		{Name: "DATASET_MAX_BYTES", Kind: KindSize, Default: "256MB", InFile: true, Description: "Size cap on one dataset. Part of the contract; unused by this tool"},
		{Name: "DATASET_MAX_RECORDS", Kind: KindInt, Default: int64(1000000), Min: 1, InFile: true, Description: "Record cap on one dataset. Part of the contract; unused by this tool"},
		{Name: "DATASET_TOTAL_BYTES", Kind: KindSize, Default: "2GB", InFile: true, Description: "Size budget for the dataset directory. Part of the contract; unused by this tool"},
		{Name: "LOG_LEVEL", Kind: KindLevel, Default: "error", InFile: true, Description: "Diagnostics on stderr: off, error, warn, info, debug. --verbose sets debug"},
		{Name: "BASE_URL", Kind: KindURL, Default: "https://api.telegram.org", InFile: true, Description: "Telegram Bot API root. Overrides are honoured only for loopback hosts"},
		{Name: "DEFAULT_CHAT", Kind: KindChat, InFile: true, Description: "Chat id used when --chat is absent. Always allowed for this profile"},
		{Name: "ALLOWED_CHATS", Kind: KindChatList, InFile: true, Description: "Comma-separated chat ids this profile may address, besides DEFAULT_CHAT"},
		{Name: "PARSE_MODE", Kind: KindEnum, Default: "none", Enum: ParseModes, InFile: true, Description: "Default text formatting for sent messages: none, html, or markdownv2"},
		{Name: "POLL_INTERVAL", Kind: KindDuration, Default: "2s", MinDuration: MinPollInterval, InFile: true, Description: "Pause between polls of the update queue while waiting (at least 500ms)"},
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
		duration, err := ParseDuration(raw)
		if err != nil {
			return nil, err
		}
		if def.MinDuration > 0 && duration < def.MinDuration {
			return nil, fmt.Errorf("%s is below the minimum of %s", raw, def.MinDuration)
		}
		return raw, nil
	case KindChat:
		if !ValidChatID(raw) {
			return nil, fmt.Errorf("%q is not a chat id (digits, optionally negative) or a public @username", raw)
		}
		return raw, nil
	case KindChatList:
		chats := SplitChats(raw)
		for _, chat := range chats {
			if !ValidChatID(chat) {
				return nil, fmt.Errorf("%q is not a chat id (digits, optionally negative) or a public @username", chat)
			}
		}
		return strings.Join(chats, ","), nil
	case KindEnum:
		value := strings.ToLower(strings.TrimSpace(raw))
		for _, allowed := range def.Enum {
			if value == allowed {
				return value, nil
			}
		}
		return nil, fmt.Errorf("%q is not one of %s", raw, strings.Join(def.Enum, ", "))
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
