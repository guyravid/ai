package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
)

// Nested blocks, contract §13.4 rule 7. The telegram tool declares none; these tests declare their own.

func nestedDefs() []*Def {
	return append(Defs(),
		&Def{Name: "TELEMETRY_OTLP_ENDPOINT", Kind: KindURL, Default: "https://otel.invalid", Flag: "--telemetry-otlp-endpoint",
			InFile: true, FilePath: []string{"telemetry", "otlp", "endpoint"}},
		&Def{Name: "TELEMETRY_OTLP_TIMEOUT", Kind: KindDuration, Default: "10s", InFile: true,
			FilePath: []string{"telemetry", "otlp", "timeout"}},
		&Def{Name: "TELEMETRY_SAMPLE_RATE", Kind: KindInt, Default: int64(100), Min: 0, InFile: true,
			FilePath: []string{"telemetry", "sample_rate"}},
	)
}

var nestedCredentialPaths = map[string][]string{"TELEMETRY_OTLP_TOKEN": {"telemetry", "otlp", "token"}}

func nestedInput(t *testing.T, flags map[string]string, env map[string]string) Input {
	t.Helper()
	if env["HOME"] == "" {
		env["HOME"] = t.TempDir()
	}
	return Input{Tool: "telegram", Prefix: "TELEGRAM", Flags: flags, Env: mapEnv(env), GOOS: "linux",
		Credentials: []string{"BOT_TOKEN", "TELEMETRY_OTLP_TOKEN"}, CredentialPaths: nestedCredentialPaths, Defs: nestedDefs()}
}

func loadNested(t *testing.T, content string, flags map[string]string, env map[string]string) (*Resolved, *errs.Error) {
	t.Helper()
	if flags == nil {
		flags = map[string]string{}
	}
	flags["config"] = writeConfig(t, content)
	if env == nil {
		env = map[string]string{}
	}
	return Load(nestedInput(t, flags, env))
}

// Rule 7.1: the path joins with "_" and uppercases to the setting's name, so its variables and flag
// stay flat; rule 7.6: list-config reports the flat name with a source pointing at the path.
func TestNestedSettingResolvesUnderItsFlatName(t *testing.T) {
	content := `{"default":{"telemetry":{"otlp":{"endpoint":"https://a.example","timeout":"5s"},"sample_rate":50}},
		"profiles":{"work":{"telemetry":{"otlp":{"endpoint":"https://b.example"}}}}}`
	cases := []struct {
		name   string
		flags  map[string]string
		env    map[string]string
		value  any
		origin Origin
		source string
	}{
		{"default section", nil, nil, "https://a.example", OriginFileDefault, "config.json#default.telemetry.otlp.endpoint"},
		{"profile section", map[string]string{"profile": "work"}, nil, "https://b.example", OriginFileProfile,
			"config.json#profiles.work.telemetry.otlp.endpoint"},
		{"flat variable", nil, map[string]string{"TELEGRAM_TELEMETRY_OTLP_ENDPOINT": "https://c.example"},
			"https://c.example", OriginEnvDefault, "TELEGRAM_TELEMETRY_OTLP_ENDPOINT"},
		{"flat profile variable", map[string]string{"profile": "work"},
			map[string]string{"TELEGRAM_TELEMETRY_OTLP_ENDPOINT_WORK": "https://d.example"},
			"https://d.example", OriginEnvProfile, "TELEGRAM_TELEMETRY_OTLP_ENDPOINT_WORK"},
		{"flat flag", map[string]string{"telemetry-otlp-endpoint": "https://e.example"}, nil,
			"https://e.example", OriginFlag, "--telemetry-otlp-endpoint"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolved, err := loadNested(t, content, tc.flags, tc.env)
			if err != nil {
				t.Fatalf("load failed: %v", err)
			}
			setting := resolved.Get("TELEMETRY_OTLP_ENDPOINT")
			if setting.Value != tc.value || setting.Origin != tc.origin || setting.Source != tc.source {
				t.Errorf("got %v from %s (%s); want %v from %s (%s)", setting.Value, setting.Source, setting.Origin,
					tc.value, tc.source, tc.origin)
			}
		})
	}

	resolved, err := loadNested(t, `{}`, nil, map[string]string{"AGENTCLI_TELEMETRY_SAMPLE_RATE": "7"})
	if err != nil {
		t.Fatal(err)
	}
	if setting := resolved.Get("TELEMETRY_SAMPLE_RATE"); setting.Value != int64(7) || setting.Origin != OriginFamily {
		t.Errorf("AGENTCLI_ variable: got %v (%s)", setting.Value, setting.Origin)
	}
}

// Rule 7.4: a profile overrides only the settings its blocks contain.
func TestNestedProfilesMergePerSetting(t *testing.T) {
	cases := []struct {
		name    string
		profile string
		want    map[string]Origin
	}{
		{"profile sets one member of a block", `{"telemetry":{"otlp":{"endpoint":"https://b.example"}}}`, map[string]Origin{
			"TELEMETRY_OTLP_ENDPOINT": OriginFileProfile,
			"TELEMETRY_OTLP_TIMEOUT":  OriginFileDefault,
			"TELEMETRY_SAMPLE_RATE":   OriginFileDefault,
		}},
		{"profile sets an outer member only", `{"telemetry":{"sample_rate":1}}`, map[string]Origin{
			"TELEMETRY_OTLP_ENDPOINT": OriginFileDefault,
			"TELEMETRY_OTLP_TIMEOUT":  OriginFileDefault,
			"TELEMETRY_SAMPLE_RATE":   OriginFileProfile,
		}},
		{"empty block changes nothing", `{"telemetry":{"otlp":{}}}`, map[string]Origin{
			"TELEMETRY_OTLP_ENDPOINT": OriginFileDefault,
			"TELEMETRY_OTLP_TIMEOUT":  OriginFileDefault,
			"TELEMETRY_SAMPLE_RATE":   OriginFileDefault,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := `{"default":{"telemetry":{"otlp":{"endpoint":"https://a.example","timeout":"5s"},"sample_rate":50}},
				"profiles":{"work":` + tc.profile + `}}`
			resolved, err := loadNested(t, content, map[string]string{"profile": "work"}, nil)
			if err != nil {
				t.Fatalf("load failed: %v", err)
			}
			for name, origin := range tc.want {
				if got := resolved.Get(name).Origin; got != origin {
					t.Errorf("%s origin %s, want %s", name, got, origin)
				}
			}
		})
	}
}

// Rules 7.3 (one spelling) and 7.5 (unknown members at any level), plus blocks that are not objects
// and invalid values inside blocks: each is config, naming the member by its full path.
func TestNestedFileRejections(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		member   string
		inHint   string
		inReason string
	}{
		{"unknown member in a block", `{"default":{"telemetry":{"otlp":{"endpiont":"https://a.example"}}}}`,
			"default.telemetry.otlp.endpiont", "list-config --schema", "unrecognised"},
		{"unknown member one level down", `{"default":{"telemetry":{"tracing":true}}}`,
			"default.telemetry.tracing", "list-config --schema", "unrecognised"},
		{"unknown block at the top", `{"default":{"metrics":{"enabled":true}}}`,
			"default.metrics", "list-config --schema", "unrecognised"},
		{"unknown member in a profile block", `{"profiles":{"work":{"telemetry":{"otlp":{"x":1}}}}}`,
			"profiles.work.telemetry.otlp.x", "list-config --schema", "unrecognised"},
		{"flat spelling of a nested setting", `{"default":{"telemetry_otlp_endpoint":"https://a.example"}}`,
			"default.telemetry_otlp_endpoint", "telemetry.otlp.endpoint", "unrecognised"},
		{"partly flat spelling of a nested setting", `{"default":{"telemetry":{"otlp_endpoint":"https://a.example"}}}`,
			"default.telemetry.otlp_endpoint", "telemetry.otlp.endpoint", "unrecognised"},
		{"nested spelling of a flat setting", `{"default":{"dataset":{"ttl":"4h"}}}`,
			"default.dataset", "list-config --schema", "unrecognised"},
		{"nested spelling of a flat setting inside a block", `{"default":{"telemetry":{"limit":5}}}`,
			"default.telemetry.limit", "list-config --schema", "unrecognised"},
		{"block holding a number", `{"default":{"telemetry":5}}`, "default.telemetry", "list-config --schema", "must be an object"},
		{"block holding null", `{"default":{"telemetry":{"otlp":null}}}`, "default.telemetry.otlp", "list-config --schema", "must be an object"},
		{"block holding an array", `{"profiles":{"work":{"telemetry":[]}}}`, "profiles.work.telemetry", "list-config --schema", "must be an object"},
		{"invalid value in a block", `{"default":{"telemetry":{"otlp":{"timeout":30}}}}`,
			"default.telemetry.otlp.timeout", "", "a bare number is not accepted"},
		{"description inside a block", `{"default":{"telemetry":{"description":"x"}}}`,
			"default.telemetry.description", "list-config --schema", "unrecognised"},
		{"env_file inside a block", `{"default":{"telemetry":{"env_file":"~/x.env"}}}`,
			"default.telemetry.env_file", "list-config --schema", "unrecognised"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadNested(t, tc.content, nil, nil)
			if err == nil || err.Code != errs.Config {
				t.Fatalf("want config, got %v", err)
			}
			if err.Details["member"] != tc.member {
				t.Errorf("member %v, want %s", err.Details["member"], tc.member)
			}
			if !strings.Contains(err.Message, tc.member) && !strings.Contains(err.Message, strings.TrimPrefix(tc.member, "default.")) {
				t.Errorf("message %q does not name %s", err.Message, tc.member)
			}
			if !strings.Contains(err.Message, tc.inReason) {
				t.Errorf("message %q does not say %q", err.Message, tc.inReason)
			}
			if !strings.Contains(err.Hint, tc.inHint) {
				t.Errorf("hint %q does not mention %q", err.Hint, tc.inHint)
			}
		})
	}
}

// Rule 7.3 in the other direction: the hint names the declared spelling when the flat name is known.
func TestNestedOneSpellingHintNamesDeclaredSpelling(t *testing.T) {
	cases := map[string]string{
		`{"default":{"telemetry_otlp_endpoint":"https://a.example"}}`:                                                     "telemetry_otlp_endpoint as telemetry.otlp.endpoint",
		`{"default":{"telemetry":{"otlp":{"token_file":"~/t"}}},"profiles":{"work":{"telemetry_otlp_token_file":"~/t"}}}`: "telemetry_otlp_token_file as telemetry.otlp.token_file",
		`{"default":{"telemetry":{"otlp_timeout":"5s"}}}`:                                                                 "telemetry.otlp_timeout as telemetry.otlp.timeout",
	}
	for content, want := range cases {
		_, err := loadNested(t, content, nil, nil)
		if err == nil || !strings.Contains(err.Hint, want) {
			t.Errorf("%s: hint %v, want it to contain %q", content, err, want)
		}
	}
}

// Rule 7.6 (credentials): inside a block a credential is only ever a <name>_file path.
func TestNestedCredentials(t *testing.T) {
	const secret = "s3cr3t-value-should-not-appear"
	_, err := loadNested(t, `{"default":{"telemetry":{"otlp":{"token":"`+secret+`"}}}}`, nil, nil)
	if err == nil || err.Code != errs.Config || err.Details["member"] != "default.telemetry.otlp.token" {
		t.Fatalf("inline nested credential: got %v", err)
	}
	if encoded, _ := json.Marshal(err); strings.Contains(string(encoded)+err.Message+err.Hint, secret) {
		t.Errorf("the credential value was echoed")
	}
	if !strings.Contains(err.Hint, "telemetry.otlp.token_file") {
		t.Errorf("hint %q does not name the file member", err.Hint)
	}

	for _, bad := range []string{`""`, `5`, `null`} {
		_, err := loadNested(t, `{"default":{"telemetry":{"otlp":{"token_file":`+bad+`}}}}`, nil, nil)
		if err == nil || err.Code != errs.Config || !strings.Contains(err.Message, "config.json#default.telemetry.otlp.token_file") {
			t.Errorf("token_file %s: got %v", bad, err)
		}
	}

	resolved, err := loadNested(t, `{"default":{"env_file":"~/d.env","telemetry":{"otlp":{"token_file":"~/default.token"}}},
		"profiles":{"work":{"telemetry":{"otlp":{"token_file":"~/work.token"}}}}}`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	file := resolved.File
	cases := []struct {
		profile, key, path, pointer string
	}{
		{"", "telemetry_otlp_token_file", "~/default.token", "config.json#default.telemetry.otlp.token_file"},
		{"work", "telemetry_otlp_token_file", "~/work.token", "config.json#profiles.work.telemetry.otlp.token_file"},
		{"", "env_file", "~/d.env", "config.json#default.env_file"},
	}
	for _, tc := range cases {
		path, pathErr := file.PathMember(tc.profile, tc.key)
		if pathErr != nil || path != tc.path || file.Pointer(tc.profile, tc.key) != tc.pointer {
			t.Errorf("%s/%s: got %q (%v) at %s", tc.profile, tc.key, path, pathErr, file.Pointer(tc.profile, tc.key))
		}
	}
}

// Rules 7.1 and 7.2: a tool's declarations must give every setting one path that flattens to its name.
func TestLayoutRejectsBadDeclarations(t *testing.T) {
	def := func(name string, path ...string) *Def {
		return &Def{Name: name, Kind: KindPath, InFile: true, FilePath: path}
	}
	cases := []struct {
		name        string
		defs        []*Def
		credentials []string
		credPaths   map[string][]string
		want        string
	}{
		{"path does not flatten to the name", []*Def{def("TELEMETRY_ENDPOINT", "telemetry", "otlp", "endpoint")}, nil, nil,
			"flattens to TELEMETRY_OTLP_ENDPOINT, not to its name TELEMETRY_ENDPOINT"},
		{"two paths flatten to one name", []*Def{def("A_B_C", "a", "b_c"), def("A_B_C", "a_b", "c")}, nil, nil,
			"both flatten to A_B_C"},
		{"nested path collides with a flat setting", []*Def{{Name: "A_B", Kind: KindPath, InFile: true}, def("A_B", "a", "b")}, nil, nil,
			"both flatten to A_B"},
		{"same nested path twice", []*Def{def("A_B", "a", "b"), def("A_B", "a", "b")}, nil, nil,
			"both flatten to A_B"},
		{"flat member is also a block", []*Def{{Name: "TELEMETRY", Kind: KindPath, InFile: true}, def("TELEMETRY_URL", "telemetry", "url")}, nil, nil,
			"telemetry is a member, so it cannot also be the block"},
		{"block is also a flat member", []*Def{def("TELEMETRY_URL", "telemetry", "url"), {Name: "TELEMETRY", Kind: KindPath, InFile: true}}, nil, nil,
			"telemetry is a block, so it cannot also be a member"},
		{"block named env_file", []*Def{def("ENV_FILE_X", "env_file", "x")}, nil, nil,
			"env_file is a block"},
		{"block named description", []*Def{def("DESCRIPTION_X", "description", "x")}, nil, nil,
			"description is a block"},
		{"nested credential file collides with a setting", []*Def{def("T_TOKEN_FILE", "t", "token_file")},
			[]string{"T_TOKEN"}, map[string][]string{"T_TOKEN": {"t", "token"}}, "both flatten to T_TOKEN_FILE"},
		{"credential path does not flatten to its name", nil,
			[]string{"T_TOKEN"}, map[string][]string{"T_TOKEN": {"t", "key"}}, "not to its name T_TOKEN"},
		{"uppercase segment", []*Def{def("A_B", "A", "b")}, nil, nil, "must be lowercase"},
		{"empty segment", []*Def{def("A__B", "a", "", "b")}, nil, nil, "must be lowercase"},
		{"nested flag that is not the flat name", []*Def{{Name: "A_B", Kind: KindPath, InFile: true, Flag: "--ab", FilePath: []string{"a", "b"}}},
			nil, nil, "its flag must be --a-b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewLayout(tc.defs, tc.credentials, tc.credPaths)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}

	in := nestedInput(t, map[string]string{}, map[string]string{})
	in.Defs = append(in.Defs, def("TELEMETRY_OTLP_ENDPOINT", "telemetry_otlp", "endpoint"))
	if _, err := Load(in); err == nil || err.Code != errs.Internal {
		t.Errorf("Load with a colliding declaration: got %v, want internal", err)
	}
}

func TestNestedLayoutAcceptsValidDeclarations(t *testing.T) {
	layout, err := NewLayout(nestedDefs(), []string{"BOT_TOKEN", "TELEMETRY_OTLP_TOKEN"}, nestedCredentialPaths)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"telemetry_otlp_endpoint":   "telemetry.otlp.endpoint",
		"telemetry_sample_rate":     "telemetry.sample_rate",
		"telemetry_otlp_token_file": "telemetry.otlp.token_file",
		"limit":                     "limit",
		"bot_token_file":            "bot_token_file",
	}
	for key, want := range cases {
		if got := layout.Path(key); got != want {
			t.Errorf("Path(%s) = %s, want %s", key, got, want)
		}
	}
	if !layout.Nested() {
		t.Errorf("Nested() = false")
	}
	flat, err := NewLayout(Defs(), []string{"BOT_TOKEN"}, nil)
	if err != nil || flat.Nested() {
		t.Errorf("the telegram layout must be valid and flat: %v", err)
	}
}

// Rule 7.6 (schema): list-config --schema shows the nesting; a flat layout leaves it unchanged.
func TestNestSchema(t *testing.T) {
	layout, err := NewLayout(nestedDefs(), []string{"BOT_TOKEN", "TELEMETRY_OTLP_TOKEN"}, nestedCredentialPaths)
	if err != nil {
		t.Fatal(err)
	}
	flat := map[string]any{
		"limit":                     "L",
		"telemetry_otlp_endpoint":   "E",
		"telemetry_otlp_timeout":    "T",
		"telemetry_sample_rate":     "S",
		"telemetry_otlp_token_file": "F",
		"env_file":                  "V",
	}
	want := map[string]any{
		"limit":    "L",
		"env_file": "V",
		"telemetry": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
			"sample_rate": "S",
			"otlp": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
				"endpoint": "E", "timeout": "T", "token_file": "F",
			}},
		}},
	}
	if got := layout.NestSchema(flat); !reflect.DeepEqual(got, want) {
		t.Errorf("NestSchema = %#v", got)
	}

	plain, _ := NewLayout(Defs(), []string{"BOT_TOKEN"}, nil)
	flatOnly := map[string]any{"limit": "L", "bot_token_file": "F", "env_file": "V", "description": "D"}
	if got := plain.NestSchema(flatOnly); !reflect.DeepEqual(got, flatOnly) {
		t.Errorf("flat NestSchema changed the properties: %#v", got)
	}
}

// Rule 7.7: a tool that declares no nested setting rejects every block as an unrecognised member.
func TestFlatToolRejectsBlocks(t *testing.T) {
	for _, content := range []string{
		`{"default":{"dataset":{"ttl":"4h"}}}`,
		`{"profiles":{"work":{"telemetry":{"otlp":{"endpoint":"https://a.example"}}}}}`,
	} {
		path := writeConfig(t, content)
		_, err := load(t, map[string]string{"config": path}, map[string]string{})
		if err == nil || err.Code != errs.Config || !strings.Contains(err.Message, "unrecognised member") {
			t.Errorf("%s: got %v", content, err)
		}
	}
	path := writeConfig(t, `{"default":{"limit":{"value":5}}}`)
	if _, err := load(t, map[string]string{"config": path}, map[string]string{}); err == nil || err.Code != errs.Config {
		t.Errorf("object as a flat setting's value: got %v", err)
	}
}

// Profiles declared through nested sections are listed like any other.
func TestNestedProfilesAreListed(t *testing.T) {
	in := nestedInput(t, map[string]string{"config": writeConfig(t,
		`{"profiles":{"work":{"description":"Work.","telemetry":{"otlp":{"endpoint":"https://b.example"}}}}}`)}, map[string]string{})
	list, err := ListProfiles(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Entries) != 2 || list.Entries[1].Name != "work" || *list.Entries[1].Description != "Work." {
		t.Errorf("entries %+v", list.Entries)
	}
}
