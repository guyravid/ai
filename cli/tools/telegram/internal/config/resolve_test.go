package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
)

func mapEnv(values map[string]string) Env {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func load(t *testing.T, flags map[string]string, env map[string]string) (*Resolved, *errs.Error) {
	t.Helper()
	if env["HOME"] == "" {
		env["HOME"] = t.TempDir()
	}
	return Load(Input{Tool: "telegram", Prefix: "TELEGRAM", Flags: flags, Env: mapEnv(env), GOOS: "linux",
		Credentials: []string{"BOT_TOKEN"}})
}

func mustLoad(t *testing.T, flags map[string]string, env map[string]string) *Resolved {
	t.Helper()
	resolved, err := load(t, flags, env)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	return resolved
}

func TestEveryTierReportsItsOrigin(t *testing.T) {
	path := writeConfig(t, `{"config_version":1,
		"default":{"max_pages":20,"base_url":"http://127.0.0.1:9000"},
		"profiles":{"work":{"base_url":"http://127.0.0.1:9001"}}}`)
	resolved := mustLoad(t,
		map[string]string{"timeout": "45s", "profile": "work", "config": path},
		map[string]string{
			"TELEGRAM_DATASET_TTL_WORK": "4h",
			"TELEGRAM_LIMIT":            "50",
			"AGENTCLI_LOG_LEVEL":        "warn",
		})

	cases := []struct {
		name   string
		value  any
		origin Origin
		source string
	}{
		{"TIMEOUT", "45s", OriginFlag, "--timeout"},
		{"DATASET_TTL", "4h", OriginEnvProfile, "TELEGRAM_DATASET_TTL_WORK"},
		{"LIMIT", int64(50), OriginEnvDefault, "TELEGRAM_LIMIT"},
		{"BASE_URL", "http://127.0.0.1:9001", OriginFileProfile, "config.json#profiles.work.base_url"},
		{"MAX_PAGES", int64(20), OriginFileDefault, "config.json#default.max_pages"},
		{"LOG_LEVEL", "warn", OriginFamily, "AGENTCLI_LOG_LEVEL"},
		{"MAX_BYTES", int64(32768), OriginBuiltin, "builtin"},
	}
	for _, tc := range cases {
		setting := resolved.Get(tc.name)
		if setting.Value != tc.value || setting.Origin != tc.origin || setting.Source != tc.source {
			t.Errorf("%s = %v from %s (%s); want %v from %s (%s)", tc.name, setting.Value, setting.Source,
				setting.Origin, tc.value, tc.source, tc.origin)
		}
	}
}

// The bug the contract warns about: selecting a profile must never reset settings it leaves out.
func TestPartialProfileKeepsLowerTiers(t *testing.T) {
	path := writeConfig(t, `{"config_version":1,"default":{"max_pages":20,"timeout":"10s"},
		"profiles":{"work":{"dataset_ttl":"4h"}}}`)
	resolved := mustLoad(t, map[string]string{"profile": "work", "config": path},
		map[string]string{"TELEGRAM_LIMIT": "40"})

	if resolved.Int("MAX_PAGES") != 20 || resolved.Get("MAX_PAGES").Origin != OriginFileDefault {
		t.Errorf("MAX_PAGES lost its file default: %+v", resolved.Get("MAX_PAGES"))
	}
	if resolved.Int("LIMIT") != 40 || resolved.Get("LIMIT").Origin != OriginEnvDefault {
		t.Errorf("LIMIT lost its env value: %+v", resolved.Get("LIMIT"))
	}
	if resolved.Duration("TIMEOUT") != 10*time.Second {
		t.Errorf("TIMEOUT = %v", resolved.Duration("TIMEOUT"))
	}
	if resolved.String("DATASET_TTL") != "4h" {
		t.Errorf("DATASET_TTL = %v", resolved.String("DATASET_TTL"))
	}
}

func TestVersionIsOptional(t *testing.T) {
	path := writeConfig(t, `{"default":{"limit":7}}`)
	resolved := mustLoad(t, map[string]string{"config": path}, map[string]string{})
	if resolved.Get("LIMIT").Origin != OriginFileDefault {
		t.Errorf("origin = %s", resolved.Get("LIMIT").Origin)
	}
}

func TestEnvBeatsFile(t *testing.T) {
	path := writeConfig(t, `{"config_version":1,"default":{"limit":10}}`)
	resolved := mustLoad(t, map[string]string{"config": path}, map[string]string{"TELEGRAM_LIMIT": "30"})
	if resolved.Get("LIMIT").Origin != OriginEnvDefault {
		t.Errorf("origin = %s", resolved.Get("LIMIT").Origin)
	}
}

func TestProfileSelection(t *testing.T) {
	path := writeConfig(t, `{"config_version":1,"profiles":{"a":{},"b":{}}}`)
	resolved := mustLoad(t, map[string]string{"config": path}, map[string]string{"TELEGRAM_PROFILE": "a"})
	if resolved.Profile != "a" {
		t.Errorf("profile = %q", resolved.Profile)
	}
	resolved = mustLoad(t, map[string]string{"config": path, "profile": "b"}, map[string]string{"TELEGRAM_PROFILE": "a"})
	if resolved.Profile != "b" {
		t.Errorf("flag should win: profile = %q", resolved.Profile)
	}
}

func TestProfileDeclaredByEnvironmentOnly(t *testing.T) {
	resolved := mustLoad(t, map[string]string{"profile": "eu-west"}, map[string]string{"TELEGRAM_TIMEOUT_EU_WEST": "5s"})
	if resolved.Get("TIMEOUT").Origin != OriginEnvProfile {
		t.Errorf("origin = %s", resolved.Get("TIMEOUT").Origin)
	}
}

func TestLoadFailures(t *testing.T) {
	cases := []struct {
		name    string
		content string
		flags   map[string]string
		code    errs.Code
	}{
		{"unknown profile", `{"config_version":1}`, map[string]string{"profile": "nope"}, errs.Config},
		{"unparseable", `{"config_version":1,`, nil, errs.Config},
		{"unknown member", `{"config_version":1,"default":{"limt":5}}`, nil, errs.Config},
		{"unknown top-level", `{"config_version":1,"extra":{}}`, nil, errs.Config},
		{"wrong version", `{"config_version":2}`, nil, errs.Config},
		{"credential without version", `{"default":{"bot_token":"abcdef1234567890"}}`, nil, errs.Config},
		{"credential value", `{"config_version":1,"profiles":{"w":{"bot_token":"abcdef1234567890"}}}`, nil, errs.Config},
		{"bare duration", `{"config_version":1,"default":{"timeout":30}}`, nil, errs.Config},
		{"FILE profile", `{"config_version":1,"profiles":{"FILE":{}}}`, nil, errs.Config},
		{"bad flag value", `{"config_version":1}`, map[string]string{"limit": "lots"}, errs.Usage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			flags := map[string]string{"config": writeConfig(t, tc.content)}
			for key, value := range tc.flags {
				flags[key] = value
			}
			_, err := load(t, flags, map[string]string{})
			if err == nil || err.Code != tc.code {
				t.Fatalf("got %v, want %s", err, tc.code)
			}
		})
	}
}

func TestCredentialValueIsNotEchoed(t *testing.T) {
	flags := map[string]string{"config": writeConfig(t, `{"config_version":1,"default":{"bot_token":"supersecretvalue123"}}`)}
	_, err := load(t, flags, map[string]string{})
	if err == nil {
		t.Fatal("expected a config error")
	}
	for _, text := range []string{err.Message, err.Hint} {
		if strings.Contains(text, "supersecretvalue123") {
			t.Fatalf("value echoed: %s", text)
		}
	}
}

func TestExplicitMissingConfigFails(t *testing.T) {
	_, err := load(t, map[string]string{"config": "/nonexistent/config.json"}, map[string]string{})
	if err == nil || err.Code != errs.Config {
		t.Fatalf("got %v", err)
	}
}

func TestDefaultConfigLoadedFromXDG(t *testing.T) {
	xdg := t.TempDir()
	dir := filepath.Join(xdg, "agentcli", "telegram")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"config_version":1,"default":{"limit":7}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, goos := range []string{"linux", "darwin"} {
		resolved, err := Load(Input{Tool: "telegram", Prefix: "TELEGRAM", Env: mapEnv(map[string]string{
			"HOME": t.TempDir(), "XDG_CONFIG_HOME": xdg}), GOOS: goos})
		if err != nil {
			t.Fatal(err)
		}
		if resolved.Int("LIMIT") != 7 {
			t.Errorf("%s: default config file not loaded", goos)
		}
	}
}

func TestNoHomeIsConfigError(t *testing.T) {
	_, err := Load(Input{Tool: "telegram", Prefix: "TELEGRAM", Env: mapEnv(map[string]string{}), GOOS: "linux"})
	if err == nil || err.Code != errs.Config {
		t.Fatalf("got %v", err)
	}
}

func TestClampWarns(t *testing.T) {
	resolved := mustLoad(t, map[string]string{"max-bytes": "99999999"}, map[string]string{})
	if resolved.Int("MAX_BYTES") != 1048576 || len(resolved.Warnings) != 1 {
		t.Fatalf("MAX_BYTES = %d, warnings = %v", resolved.Int("MAX_BYTES"), resolved.Warnings)
	}
}

func TestUnits(t *testing.T) {
	if _, err := ParseDuration("30"); err == nil {
		t.Error("bare number accepted as duration")
	}
	if duration, err := ParseDuration("7d"); err != nil || duration != 7*24*time.Hour {
		t.Errorf("7d = %v, %v", duration, err)
	}
	if size, err := ParseSize("256MB"); err != nil || size != 256_000_000 {
		t.Errorf("256MB = %d, %v", size, err)
	}
	if _, err := ParseSize("256"); err == nil {
		t.Error("bare number accepted as size")
	}
}
