package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guyravid/ai/cli/tools/telegram/internal/config"
	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
)

const (
	defaultToken = "111111111:AAdefaultBotTokenValue0000000000000000"
	profileToken = "222222222:AAbuildsBotTokenValue00000000000000000"
)

// runIsolation writes a config file and token files, then resolves BOT_TOKEN for the profile and
// applies the isolation check. member is the JSON of the extra config members.
type isolationCase struct {
	name        string
	profile     string
	env         map[string]string
	defaultJSON string // extra members of the default section; %s is the default token file
	buildsJSON  string // members of profiles.builds; %s is the profile token file
	envFile     string // content of default.env_file when the case uses one
	refused     bool   // nothing scoped resolved but a default-scope source exists
	source      string // the source expected to win, or, when refused, the default-scope one that is skipped
	origin      config.Origin
}

func runIsolation(t *testing.T, tc isolationCase) (*Credential, *errs.Error) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	defaultFile := write("default.token", defaultToken)
	profileFile := write("builds.token", profileToken)
	envFile := write("default.env", tc.envFile)
	env := map[string]string{"HOME": dir}
	for key, value := range tc.env {
		env[key] = strings.NewReplacer("{default}", defaultFile, "{profile}", profileFile).Replace(value)
	}
	defaultSection := strings.NewReplacer("{default}", defaultFile, "{env}", envFile).Replace(tc.defaultJSON)
	buildsSection := strings.NewReplacer("{profile}", profileFile, "{env}", envFile).Replace(tc.buildsJSON)
	content := fmt.Sprintf(`{"config_version":1,"default":{%s},"profiles":{"builds":{%s}}}`, defaultSection, buildsSection)
	path := write("config.json", content)
	file, loadErr := config.LoadFile(path, true, config.Defs(), []string{"BOT_TOKEN"})
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	credentials, _, err := Resolve(Input{Prefix: "TELEGRAM", Profile: tc.profile, Env: envOf(env), GOOS: "linux",
		File: file, Names: []string{"BOT_TOKEN"}, ScopedOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	credential := credentials[0]
	return credential, CheckProfileIsolation(credential, "TELEGRAM", tc.profile)
}

func TestProfileTokenResolvesOnlyThroughProfileScopedSources(t *testing.T) {
	cases := []isolationCase{
		// The profile's own sources, in the order the owner fixed.
		{name: "1 profile variable", profile: "builds", env: map[string]string{"TELEGRAM_BOT_TOKEN_BUILDS": profileToken},
			source: "TELEGRAM_BOT_TOKEN_BUILDS", origin: config.OriginEnvProfile},
		{name: "2 profile file variable", profile: "builds", env: map[string]string{"TELEGRAM_BOT_TOKEN_FILE_BUILDS": "{profile}"},
			source: "TELEGRAM_BOT_TOKEN_FILE_BUILDS", origin: config.OriginEnvProfile},
		{name: "3 profile bot_token_file", profile: "builds", buildsJSON: `"bot_token_file":"{profile}"`,
			source: "config.json#profiles.builds.bot_token_file", origin: config.OriginFileProfile},
		{name: "3 profile env_file with the unsuffixed key", profile: "builds", buildsJSON: `"env_file":"{env}"`,
			envFile: "TELEGRAM_BOT_TOKEN=" + profileToken + "\n", source: "config.json#profiles.builds.env_file", origin: config.OriginFileProfile},
		{name: "4 default env_file line keyed for the profile", profile: "builds", defaultJSON: `"env_file":"{env}"`,
			envFile: "TELEGRAM_BOT_TOKEN_BUILDS=" + profileToken + "\n", source: "config.json#default.env_file", origin: config.OriginFileDefault},
		{name: "4 default env_file with both keys uses the profile's", profile: "builds", defaultJSON: `"env_file":"{env}"`,
			envFile: "TELEGRAM_BOT_TOKEN=" + defaultToken + "\nTELEGRAM_BOT_TOKEN_BUILDS=" + profileToken + "\n",
			source:  "config.json#default.env_file", origin: config.OriginFileDefault},

		// The default-scope sources are skipped, so with nothing of the profile's own they are a config error.
		{name: "default-scope variable only", profile: "builds", env: map[string]string{"TELEGRAM_BOT_TOKEN": defaultToken},
			refused: true, source: "TELEGRAM_BOT_TOKEN"},
		{name: "default-scope file variable only", profile: "builds", env: map[string]string{"TELEGRAM_BOT_TOKEN_FILE": "{default}"},
			refused: true, source: "TELEGRAM_BOT_TOKEN_FILE"},
		{name: "default bot_token_file only", profile: "builds", defaultJSON: `"bot_token_file":"{default}"`, buildsJSON: `"parse_mode":"html"`,
			refused: true, source: "config.json#default.bot_token_file"},
		{name: "default env_file with the unsuffixed key only", profile: "builds", defaultJSON: `"env_file":"{env}"`,
			envFile: "TELEGRAM_BOT_TOKEN=" + defaultToken + "\n", refused: true, source: "config.json#default.env_file"},

		// The point of resolving instead of checking the winner: a profile's own source beats an
		// default-scope one that is set, whatever §12.1 would have ranked first.
		{name: "default-scope file variable + profile bot_token_file: the profile's file", profile: "builds",
			env: map[string]string{"TELEGRAM_BOT_TOKEN_FILE": "{default}"}, buildsJSON: `"bot_token_file":"{profile}"`,
			source: "config.json#profiles.builds.bot_token_file", origin: config.OriginFileProfile},
		{name: "default-scope variable + profile bot_token_file: the profile's file", profile: "builds",
			env: map[string]string{"TELEGRAM_BOT_TOKEN": defaultToken}, buildsJSON: `"bot_token_file":"{profile}"`,
			source: "config.json#profiles.builds.bot_token_file", origin: config.OriginFileProfile},
		{name: "default-scope variable + profile variable: the profile's variable", profile: "builds",
			env:    map[string]string{"TELEGRAM_BOT_TOKEN": defaultToken, "TELEGRAM_BOT_TOKEN_BUILDS": profileToken},
			source: "TELEGRAM_BOT_TOKEN_BUILDS", origin: config.OriginEnvProfile},
		{name: "default bot_token_file + profile file variable: the profile's", profile: "builds",
			env: map[string]string{"TELEGRAM_BOT_TOKEN_FILE_BUILDS": "{profile}"}, defaultJSON: `"bot_token_file":"{default}"`,
			source: "TELEGRAM_BOT_TOKEN_FILE_BUILDS", origin: config.OriginEnvProfile},
		{name: "profile variable beats the profile's own file", profile: "builds",
			env: map[string]string{"TELEGRAM_BOT_TOKEN_BUILDS": profileToken}, buildsJSON: `"bot_token_file":"{profile}"`,
			source: "TELEGRAM_BOT_TOKEN_BUILDS", origin: config.OriginEnvProfile},

		// With no profile every source is allowed, in the full §12.1 order: that is the default bot.
		{name: "no profile, default-scope variable", env: map[string]string{"TELEGRAM_BOT_TOKEN": defaultToken},
			source: "TELEGRAM_BOT_TOKEN", origin: config.OriginEnvDefault},
		{name: "no profile, default file", defaultJSON: `"bot_token_file":"{default}"`,
			source: "config.json#default.bot_token_file", origin: config.OriginFileDefault},
		{name: "no profile, default-scope variable beats the default file", env: map[string]string{"TELEGRAM_BOT_TOKEN": defaultToken},
			defaultJSON: `"bot_token_file":"{default}"`, source: "TELEGRAM_BOT_TOKEN", origin: config.OriginEnvDefault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			credential, err := runIsolation(t, tc)
			if tc.refused {
				if credential.Set || credential.WouldUse != tc.source || err == nil {
					t.Fatalf("set=%v would-use=%q err=%v, want unset and refused naming %q", credential.Set, credential.WouldUse, err, tc.source)
				}
				if err.Code != errs.Config {
					t.Errorf("code = %s, want config", err.Code)
				}
				if err.Details["would_use_source"] != tc.source || err.Details["profile"] != "builds" {
					t.Errorf("details = %v", err.Details)
				}
				for _, text := range []string{err.Message, err.Hint, fmt.Sprint(err.Details)} {
					if strings.Contains(text, defaultToken) || strings.Contains(text, defaultToken[:12]) {
						t.Errorf("a token value leaked: %s", text)
					}
				}
				if !strings.Contains(err.Message, "Profile 'builds' has no bot token of its own and would use the default bot's.") {
					t.Errorf("message = %q", err.Message)
				}
				// One hint for every origin: with the default-scope source skipped, the profile's own
				// file now wins whatever is exported.
				if !strings.Contains(err.Hint, "profiles.builds.bot_token_file") || !strings.Contains(err.Hint, "TELEGRAM_BOT_TOKEN_FILE_BUILDS") {
					t.Errorf("hint = %q", err.Hint)
				}
				return
			}
			if err != nil || !credential.Set || credential.Source != tc.source || credential.Origin != tc.origin {
				t.Fatalf("source %q origin %q set %v err %v; want %q %q", credential.Source, credential.Origin, credential.Set, err, tc.source, tc.origin)
			}
			if tc.profile != "" && credential.Secret.Reveal() != profileToken {
				t.Errorf("a selected profile must end up with its own token")
			}
			if tc.profile == "" && credential.Secret.Reveal() != defaultToken {
				t.Errorf("no profile: the default bot's token is used")
			}
		})
	}
}

func TestProfileTokenWithNothingAtAllIsNotARefusal(t *testing.T) {
	credential, err := runIsolation(t, isolationCase{profile: "builds"})
	if credential.Set || err != nil || credential.WouldUse != "" {
		t.Fatalf("nothing anywhere is the ordinary missing credential (auth), not isolation's: set=%v would=%q err=%v",
			credential.Set, credential.WouldUse, err)
	}
}

// A profile that has its own source never opens the default section's files: they are skipped, so a
// missing or unreadable default file cannot break it, and nothing is read just to be redacted.
func TestProfileTokenNeverOpensTheDefaultSectionsFiles(t *testing.T) {
	credential, err := runIsolation(t, isolationCase{profile: "builds",
		defaultJSON: `"bot_token_file":"/nonexistent/default.token","env_file":"/nonexistent/default.env"`,
		buildsJSON:  `"bot_token_file":"{profile}"`})
	if err != nil || !credential.Set || credential.Secret.Reveal() != profileToken {
		t.Fatalf("set=%v err=%v", credential.Set, err)
	}
	if len(credential.Skipped) != 0 {
		t.Errorf("the default section is not even consulted once the profile resolved: %+v", credential.Skipped)
	}
}

// When nothing scoped resolves, the default bot_token_file is named as the skipped source, by its
// pointer only: it is not opened (its path does not exist here), so no value is held.
func TestSkippedDefaultFileIsNamedWithoutBeingOpened(t *testing.T) {
	credential, err := runIsolation(t, isolationCase{profile: "builds", defaultJSON: `"bot_token_file":"/nonexistent/default.token"`,
		buildsJSON: `"parse_mode":"html"`})
	if err == nil || credential.WouldUse != "config.json#default.bot_token_file" {
		t.Fatalf("would-use %q err %v", credential.WouldUse, err)
	}
	if len(credential.Skipped) != 1 || !credential.Skipped[0].Value.IsZero() {
		t.Errorf("no value is held for a file that was not opened: %+v", credential.Skipped)
	}
}

// The values the tool did read from a skipped source are kept so the redactor still covers them.
func TestSkippedValuesAlreadyReadAreKeptForRedaction(t *testing.T) {
	credential, _ := runIsolation(t, isolationCase{profile: "builds",
		env: map[string]string{"TELEGRAM_BOT_TOKEN": defaultToken, "TELEGRAM_BOT_TOKEN_BUILDS": profileToken}})
	if len(credential.Skipped) != 1 || credential.Skipped[0].Value.Reveal() != defaultToken {
		t.Fatalf("skipped: %+v", credential.Skipped)
	}
	credential, _ = runIsolation(t, isolationCase{profile: "builds", defaultJSON: `"env_file":"{env}"`,
		envFile: "TELEGRAM_BOT_TOKEN=" + defaultToken + "\n"})
	if len(credential.Skipped) != 1 || credential.Skipped[0].Value.Reveal() != defaultToken {
		t.Errorf("the default env_file was read for the profile's key, so its unsuffixed value is covered: %+v", credential.Skipped)
	}
}

func TestProfileIsolationUsesProfileSegmentForHyphenatedNames(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "default.env")
	if err := os.WriteFile(envFile, []byte("TELEGRAM_BOT_TOKEN_EU_WEST="+profileToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.json")
	content := fmt.Sprintf(`{"config_version":1,"default":{"env_file":%q},"profiles":{"eu-west":{"parse_mode":"html"}}}`, envFile)
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	file, loadErr := config.LoadFile(configPath, true, config.Defs(), []string{"BOT_TOKEN"})
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	credentials, _, err := Resolve(Input{Prefix: "TELEGRAM", Profile: "eu-west", Env: envOf(map[string]string{"HOME": dir}), GOOS: "linux",
		File: file, Names: []string{"BOT_TOKEN"}, ScopedOnly: true})
	if err != nil || !credentials[0].Set || credentials[0].EnvFileKey != "TELEGRAM_BOT_TOKEN_EU_WEST" {
		t.Fatalf("a key naming the profile (eu-west -> EU_WEST) should resolve: %+v %v", credentials[0], err)
	}
}
