package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// botTokenRow returns list-config's BOT_TOKEN entry.
func botTokenRow(t *testing.T, r result) map[string]any {
	t.Helper()
	for _, entry := range r.object()["settings"].([]any) {
		if item := entry.(map[string]any); item["name"] == "BOT_TOKEN" {
			return item
		}
	}
	t.Fatalf("no BOT_TOKEN row: %s", r.raw)
	return nil
}

// A selected profile resolves its token only through its own sources, and the fake upstream sees
// the token it actually got (the path segment after /bot).
func TestProfileTokenSelectionEndToEnd(t *testing.T) {
	envFile := func(t *testing.T, content string) string {
		path := filepath.Join(t.TempDir(), "default.env")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	cases := []struct {
		name       string
		env        map[string]string // on top of the harness's TELEGRAM_BOT_TOKEN, which is removed first
		defaultCfg string            // extra members of the default section
		buildsCfg  func(t *testing.T) string
		profile    bool
		wantExit   int
		wantToken  string // the path segment the upstream must see
		source     string // list-config's BOT_TOKEN source
		origin     string
		set        bool
	}{
		{name: "default-scope file variable set, profile has its own file: the profile's",
			env: map[string]string{"TELEGRAM_BOT_TOKEN_FILE": "{test}"}, profile: true, wantToken: profileToken,
			buildsCfg: func(t *testing.T) string { return fmt.Sprintf(`"bot_token_file":%q`, writeTokenFile(t, profileToken)) },
			source:    "config.json#profiles.builds.bot_token_file", origin: "file_profile", set: true},
		{name: "default-scope variable + profile-scoped variable: the profile's",
			env: map[string]string{"TELEGRAM_BOT_TOKEN": testToken, "TELEGRAM_BOT_TOKEN_BUILDS": profileToken}, profile: true, wantToken: profileToken,
			source: "TELEGRAM_BOT_TOKEN_BUILDS", origin: "env_profile", set: true},
		{name: "profile-scoped file variable",
			env: map[string]string{"TELEGRAM_BOT_TOKEN_FILE_BUILDS": "{profile}"}, profile: true, wantToken: profileToken,
			source: "TELEGRAM_BOT_TOKEN_FILE_BUILDS", origin: "env_profile", set: true},
		{name: "default-scope variable only: config, zero requests",
			env: map[string]string{"TELEGRAM_BOT_TOKEN": testToken}, profile: true, wantExit: 3,
			source: "builtin", origin: "builtin"},
		{name: "default-scope file variable only: config, zero requests",
			env: map[string]string{"TELEGRAM_BOT_TOKEN_FILE": "{test}"}, profile: true, wantExit: 3,
			source: "builtin", origin: "builtin"},
		{name: "nothing anywhere: the ordinary auth error",
			profile: true, wantExit: 4, source: "builtin", origin: "builtin"},
		{name: "no profile: the default-scope variable is used",
			env: map[string]string{"TELEGRAM_BOT_TOKEN": testToken}, wantToken: testToken,
			source: "TELEGRAM_BOT_TOKEN", origin: "env_default", set: true},
		{name: "default env_file line keyed for the profile is the exception",
			profile: true, wantToken: profileToken,
			defaultCfg: `"env_file":"{envfile-keyed}"`,
			source:     "config.json#default.env_file", origin: "file_default", set: true},
		{name: "default env_file with only the unsuffixed key is skipped",
			profile: true, wantExit: 3, defaultCfg: `"env_file":"{envfile-unsuffixed}"`,
			source: "builtin", origin: "builtin"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			delete(h.env, "TELEGRAM_BOT_TOKEN")
			replacer := strings.NewReplacer("{test}", writeTokenFile(t, testToken), "{profile}", writeTokenFile(t, profileToken),
				"{envfile-keyed}", envFile(t, "TELEGRAM_BOT_TOKEN_BUILDS="+profileToken+"\n"),
				"{envfile-unsuffixed}", envFile(t, "TELEGRAM_BOT_TOKEN="+testToken+"\n"))
			for key, value := range tc.env {
				h.env[key] = replacer.Replace(value)
			}
			members := `"parse_mode":"html"`
			if tc.buildsCfg != nil {
				members = tc.buildsCfg(t)
			}
			config := writeConfigFile(t, fmt.Sprintf(`{"config_version":1,"default":{%s},"profiles":{"builds":{"description":"CI bot.",%s}}}`,
				replacer.Replace(tc.defaultCfg), members))
			args := []string{"--config", config}
			if tc.profile {
				args = append(args, "--profile", "builds")
			}

			result := h.run(append([]string{"bot", "get"}, args...)...)
			wantExit := tc.wantExit
			if result.exit != wantExit {
				t.Fatalf("exit %d, want %d: %s", result.exit, wantExit, result.raw)
			}
			if tc.wantToken != "" {
				if requests := h.fake.all(); len(requests) != 1 || requests[0].token != tc.wantToken {
					t.Errorf("the upstream must see the expected token's path segment: %+v", requests)
				}
			} else if h.fake.total() != 0 {
				t.Errorf("%d upstream requests, want 0", h.fake.total())
			}
			if wantExit == 3 {
				if h.run(append([]string{"bot", "get", "--dry-run"}, args...)...).exit != 3 || h.fake.total() != 0 {
					t.Error("--dry-run is refused the same way, with no request")
				}
			}
			for _, secret := range []string{testToken, profileToken} {
				if strings.Contains(result.raw, secret) || strings.Contains(result.stderr, secret) {
					t.Errorf("a token leaked: %s", result.raw)
				}
			}

			row := botTokenRow(t, h.run(append([]string{"list-config"}, args...)...))
			if row["source"] != tc.source || row["origin"] != tc.origin || row["set"] != tc.set {
				t.Errorf("list-config BOT_TOKEN row %v, want source %s origin %s set %v", row, tc.source, tc.origin, tc.set)
			}
			if wantExit == 3 && !strings.Contains(fmt.Sprint(row["hint"]), "profiles.builds.bot_token_file") {
				t.Errorf("the row's hint says how to fix it: %v", row["hint"])
			}
			if tc.set && row["hint"] != nil {
				t.Errorf("a resolved token has no hint: %v", row["hint"])
			}
		})
	}
}

// The redactor still covers a default-scope token the tool read from the environment, even though it
// is skipped for the profile: a stray echo of it must not get out.
func TestSkippedEnvironmentTokenIsStillRedacted(t *testing.T) {
	h := newHarness(t)
	h.env["TELEGRAM_BOT_TOKEN"] = testToken
	h.env["TELEGRAM_BOT_TOKEN_BUILDS"] = profileToken
	h.fake.override["getMe"] = func(int) (int, string) {
		return 400, fmt.Sprintf(`{"ok":false,"error_code":400,"description":"Bad Request: echoed %s and %s"}`, testToken, profileToken)
	}
	path := profileSetup(t, `"parse_mode":"html"`)
	result := h.run("bot", "get", "--profile", "builds", "--config", path)
	if result.exit != 6 || strings.Contains(result.raw, testToken) || strings.Contains(result.raw, profileToken) {
		t.Errorf("exit %d: %s", result.exit, result.raw)
	}
}

func TestDoctorReportsTheScopedSourceItUsed(t *testing.T) {
	h := newHarness(t)
	h.env["TELEGRAM_BOT_TOKEN"] = testToken
	h.env["TELEGRAM_BOT_TOKEN_BUILDS"] = profileToken
	path := profileSetup(t, `"parse_mode":"html"`)
	result := h.run("doctor", "--profile", "builds", "--config", path)
	if result.exit != 0 {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
	credentials := result.object()["checks"].([]any)[1].(map[string]any)
	detail := fmt.Sprint(credentials["detail"])
	if credentials["status"] != "pass" || !strings.Contains(detail, "TELEGRAM_BOT_TOKEN_BUILDS") || strings.Contains(detail, "from TELEGRAM_BOT_TOKEN ") {
		t.Errorf("credentials check %v", credentials)
	}
	for _, request := range h.fake.all() {
		if request.token != profileToken {
			t.Errorf("doctor used the wrong token for the profile")
		}
	}
}
