package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
)

func profileInput(t *testing.T, env map[string]string, flags map[string]string) Input {
	t.Helper()
	return Input{Tool: "telegram", Prefix: "TELEGRAM", Flags: flags, GOOS: "linux", Credentials: []string{"BOT_TOKEN"},
		Env: func(name string) (string, bool) { value, ok := env[name]; return value, ok }}
}

func pairs(env map[string]string) []string {
	var list []string
	for key, value := range env {
		list = append(list, key+"="+value)
	}
	return list
}

func names(list *ProfileList) []string {
	var out []string
	for _, entry := range list.Entries {
		out = append(out, entry.Name)
	}
	return out
}

func TestListProfilesEmptyWhenNoneDeclared(t *testing.T) {
	env := map[string]string{"HOME": t.TempDir(), "TELEGRAM_LIMIT": "5", "TELEGRAM_BOT_TOKEN_FILE": "/k", "TELEGRAM_PROFILE": ""}
	list, err := ListProfiles(profileInput(t, env, map[string]string{}), pairs(env))
	if err != nil {
		t.Fatal(err)
	}
	if list.Entries == nil || len(list.Entries) != 0 || list.Undeclared != "" {
		t.Errorf("got %+v", list)
	}
}

func TestListProfilesMergesFileAndEnvironment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"profiles":{"work":{"limit":8},"Eu-West":{"limit":9}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"HOME":                             dir,
		"TELEGRAM_TIMEOUT_WORK":            "5s",  // same profile as the file's work
		"TELEGRAM_TIMEOUT_EU_WEST":         "5s",  // same segment as the file's Eu-West
		"TELEGRAM_BOT_TOKEN_FILE_PERSONAL": "/k",  // credential file variable: longest match is BOT_TOKEN_FILE
		"TELEGRAM_MAX_BYTES_SMALL":         "100", // MAX_BYTES, not MAX
		"TELEGRAM_BOT_TOKEN_FILE":          "/k",  // no profile segment
		"TELEGRAM_LIMIT_EMPTY":             "",    // empty values declare nothing
		"TELEGRAM_MCP_TOKEN":               "x",   // not a setting
		"OTHER_LIMIT_NOPE":                 "1",
	}
	list, err := ListProfiles(profileInput(t, env, map[string]string{"config": path, "profile": "work"}), pairs(env))
	if err != nil {
		t.Fatal(err)
	}
	if got := names(list); !reflect.DeepEqual(got, []string{"default", "Eu-West", "personal", "small", "work"}) {
		t.Fatalf("names %v", got)
	}
	want := map[string][]string{
		"default": {}, "Eu-West": {"config_file", "environment"}, "personal": {"environment"},
		"small": {"environment"}, "work": {"config_file", "environment"},
	}
	for _, entry := range list.Entries {
		if !reflect.DeepEqual(entry.DeclaredIn, want[entry.Name]) {
			t.Errorf("%s declared_in %v, want %v", entry.Name, entry.DeclaredIn, want[entry.Name])
		}
		if entry.Active != (entry.Name == "work") {
			t.Errorf("%s active %v", entry.Name, entry.Active)
		}
	}
}

func TestListProfilesUndeclaredSelectionIsNotAnError(t *testing.T) {
	env := map[string]string{"HOME": t.TempDir(), "TELEGRAM_LIMIT_WORK": "5", "TELEGRAM_PROFILE": "wrok"}
	list, err := ListProfiles(profileInput(t, env, map[string]string{}), pairs(env))
	if err != nil {
		t.Fatal(err)
	}
	if list.Undeclared != "wrok" || list.Entries[0].Active || list.Entries[1].Active {
		t.Errorf("got %+v", list)
	}
}

func TestDefaultIsNotAProfileName(t *testing.T) {
	for _, name := range []string{"default", "DEFAULT", "file"} {
		if ValidateProfileName(name) == nil {
			t.Errorf("%s accepted", name)
		}
	}
	env := map[string]string{"HOME": t.TempDir(), "TELEGRAM_LIMIT_DEFAULT": "5"}
	list, err := ListProfiles(profileInput(t, env, map[string]string{}), pairs(env))
	if err != nil || len(list.Entries) != 0 {
		t.Errorf("a DEFAULT segment must not declare a profile: %+v %v", list, err)
	}
}

func writeProfileConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func descriptionOf(t *testing.T, list *ProfileList, name string) *string {
	t.Helper()
	for _, entry := range list.Entries {
		if entry.Name == name {
			return entry.Description
		}
	}
	t.Fatalf("no entry named %s in %v", name, names(list))
	return nil
}

func TestListProfilesCarriesDescriptions(t *testing.T) {
	path := writeProfileConfig(t, `{"default":{"description":"Day to day."},"profiles":{"work":{"description":"Team boards."},"plain":{"limit":5}}}`)
	dir := filepath.Dir(path)
	env := map[string]string{"HOME": dir, "TELEGRAM_LIMIT_ENVONLY": "6", "TELEGRAM_LIMIT_WORK": "7"}
	list, err := ListProfiles(profileInput(t, env, map[string]string{"config": path}), pairs(env))
	if err != nil {
		t.Fatal(err)
	}
	if got := names(list); !reflect.DeepEqual(got, []string{"default", "envonly", "plain", "work"}) {
		t.Fatalf("names %v", got)
	}
	want := map[string]*string{"default": ptr("Day to day."), "work": ptr("Team boards."), "plain": nil, "envonly": nil}
	for name, expected := range want {
		got := descriptionOf(t, list, name)
		switch {
		case expected == nil && got != nil:
			t.Errorf("%s description %q, want null", name, *got)
		case expected != nil && (got == nil || *got != *expected):
			t.Errorf("%s description %v, want %q", name, got, *expected)
		}
	}
}

func TestDefaultDescriptionIsNullWhenAbsent(t *testing.T) {
	path := writeProfileConfig(t, `{"profiles":{"work":{"description":"Team boards."}}}`)
	env := map[string]string{"HOME": filepath.Dir(path)}
	list, err := ListProfiles(profileInput(t, env, map[string]string{"config": path}), pairs(env))
	if err != nil {
		t.Fatal(err)
	}
	if descriptionOf(t, list, "default") != nil {
		t.Errorf("default without default.description must be null")
	}
}

func TestDescriptionIsNotASetting(t *testing.T) {
	for _, def := range Defs() {
		if def.FileKey() == DescriptionKey {
			t.Fatalf("description must not be a setting: %+v", def)
		}
	}
	path := writeProfileConfig(t, `{"default":{"description":"Day to day.","limit":9},"profiles":{"work":{"description":"Team boards."}}}`)
	file, err := LoadFile(path, true, Defs(), []string{"BOT_TOKEN"})
	if err != nil {
		t.Fatal(err)
	}
	if got := file.Description("work"); got == nil || *got != "Team boards." {
		t.Errorf("work description %v", got)
	}
	if got := file.Description("missing"); got != nil {
		t.Errorf("an unknown profile has no description, got %q", *got)
	}
}

func TestInvalidDescriptionsAreConfigErrors(t *testing.T) {
	cases := map[string]string{
		"too long":           `{"profiles":{"work":{"description":"` + strings.Repeat("x", 201) + `"}}}`,
		"too long (default)": `{"default":{"description":"` + strings.Repeat("x", 201) + `"}}`,
		"newline":            `{"profiles":{"work":{"description":"one\ntwo"}}}`,
		"carriage return":    `{"default":{"description":"one\rtwo"}}`,
		"number":             `{"profiles":{"work":{"description":7}}}`,
		"null":               `{"profiles":{"work":{"description":null}}}`,
		"object":             `{"default":{"description":{"a":"b"}}}`,
		"empty":              `{"default":{"description":""}}`,
		"blank":              `{"profiles":{"work":{"description":"   \t"}}}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeProfileConfig(t, content)
			_, err := LoadFile(path, true, Defs(), []string{"BOT_TOKEN"})
			if err == nil {
				t.Fatal("accepted")
			}
			if err.Code != errs.Config || err.ExitCode() != 3 {
				t.Errorf("got %v exit %d, want config exit 3", err.Code, err.ExitCode())
			}
			if !strings.Contains(err.Hint, "config.json#") || !strings.Contains(err.Hint, ".description") {
				t.Errorf("the hint must name the member pointer: %q", err.Hint)
			}
		})
	}
}

func TestDescriptionBoundaryLengthsAreAccepted(t *testing.T) {
	for _, text := range []string{"x", strings.Repeat("x", 200), strings.Repeat("é", 200), "  padded  "} {
		path := writeProfileConfig(t, `{"default":{"description":"`+text+`"}}`)
		if _, err := LoadFile(path, true, Defs(), []string{"BOT_TOKEN"}); err != nil {
			t.Errorf("%q rejected: %v", text, err.Message)
		}
	}
}

func ptr(text string) *string { return &text }
