package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func profileInput(t *testing.T, env map[string]string, flags map[string]string) Input {
	t.Helper()
	return Input{Tool: "trello", Prefix: "TRELLO", Flags: flags, GOOS: "linux", Credentials: []string{"API_KEY", "API_TOKEN"},
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
	env := map[string]string{"HOME": t.TempDir(), "TRELLO_LIMIT": "5", "TRELLO_API_KEY_FILE": "/k", "TRELLO_PROFILE": ""}
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
		"HOME":                         dir,
		"TRELLO_TIMEOUT_WORK":          "5s",  // same profile as the file's work
		"TRELLO_TIMEOUT_EU_WEST":       "5s",  // same segment as the file's Eu-West
		"TRELLO_API_KEY_FILE_PERSONAL": "/k",  // credential file variable: longest match is API_KEY_FILE
		"TRELLO_MAX_BYTES_SMALL":       "100", // MAX_BYTES, not MAX
		"TRELLO_API_KEY_FILE":          "/k",  // no profile segment
		"TRELLO_LIMIT_EMPTY":           "",    // empty values declare nothing
		"TRELLO_MCP_TOKEN":             "x",   // not a setting
		"OTHER_LIMIT_NOPE":             "1",
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
	env := map[string]string{"HOME": t.TempDir(), "TRELLO_LIMIT_WORK": "5", "TRELLO_PROFILE": "wrok"}
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
	env := map[string]string{"HOME": t.TempDir(), "TRELLO_LIMIT_DEFAULT": "5"}
	list, err := ListProfiles(profileInput(t, env, map[string]string{}), pairs(env))
	if err != nil || len(list.Entries) != 0 {
		t.Errorf("a DEFAULT segment must not declare a profile: %+v %v", list, err)
	}
}
