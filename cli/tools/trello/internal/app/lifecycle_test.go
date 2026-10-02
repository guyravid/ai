package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/guyravid/ai/cli/tools/trello/internal/commands"
	"github.com/guyravid/ai/cli/tools/trello/internal/registry"
	"github.com/guyravid/ai/cli/tools/trello/internal/shape"
)

func TestInterruptedRunIsCanceled(t *testing.T) {
	for _, code := range []int{130, 143} {
		h := newHarness(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // the signal handler cancels the context before Run looks at the result
		var stdout bytes.Buffer
		exit := h.app.Run(ctx, []string{"boards", "list"}, &stdout, func() int { return code })
		if exit != code {
			t.Errorf("exit %d, want %d", exit, code)
		}
		if !strings.Contains(stdout.String(), `"code":"canceled"`) || strings.Count(stdout.String(), "\n") != 1 {
			t.Errorf("signal %d: %s", code, stdout.String())
		}
		if !strings.Contains(stdout.String(), `"command":"boards.list"`) {
			t.Errorf("a canceled call names its command: %s", stdout.String())
		}
	}
}

type noInput struct{}

func TestCrashingHandlerIsInternal(t *testing.T) {
	h := newHarness(t)
	boom := registry.Object[noInput, map[string]string](registry.Spec{Name: "boom.get", Description: "Crash.",
		Fields: &registry.FieldSet{Default: []string{"id"}, Available: []string{"id"}}},
		func(context.Context, *registry.Call, noInput) (shape.Value, error) { panic("handler bug") })
	h.app.Registry = registry.New(append(registry.Builtins(), boom))
	result := h.run("boom", "get")
	if result.exit != 1 || result.errorCode() != "internal" {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
	if result.doc["command"] != "boom.get" {
		t.Errorf("a crashed call names its command: %v", result.doc["command"])
	}
	if !strings.Contains(result.raw, "handler bug") || strings.Contains(result.raw, "goroutine") {
		t.Errorf("message should name the panic without a stack: %s", result.raw)
	}
}

func TestCanceledWithoutACommandIsNull(t *testing.T) {
	h := newHarness(t)
	var stdout bytes.Buffer
	h.app.Run(context.Background(), []string{"no", "such"}, &stdout, func() int { return 130 })
	if !strings.Contains(stdout.String(), `"command":null`) {
		t.Errorf("an unrecognised command stays null: %s", stdout.String())
	}
}

func TestStartupErrorIsInternal(t *testing.T) {
	h := newHarness(t)
	h.app.StartupError = errorString("command x: bad")
	if result := h.run("boards", "list"); result.exit != 1 || result.errorCode() != "internal" || h.fake.total() != 0 {
		t.Errorf("exit %d after %d requests: %s", result.exit, h.fake.total(), result.raw)
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }

// mutatingMention matches a mutating command in either form, dotted or as a command line.
func mutatingMention() *regexp.Regexp {
	var alternatives []string
	for _, name := range commands.MutatingNames {
		group, verb, _ := strings.Cut(name, ".")
		alternatives = append(alternatives, regexp.QuoteMeta(group)+`[. ]`+regexp.QuoteMeta(verb)+`\b`)
	}
	return regexp.MustCompile(strings.Join(alternatives, "|"))
}

func TestReadOnlyBuildHidesWrites(t *testing.T) {
	if commands.WritesEnabled {
		t.Skip("full build")
	}
	h := newHarness(t)
	mention := mutatingMention()
	surfaces := []string{h.run("tools").raw, h.run("tools", "--detail").raw, h.run("describe").raw, h.run("teach").raw,
		h.run("teach", "--list").raw}
	for _, line := range strings.Split(h.run("teach", "--list").raw, "\n") {
		if topic := regexp.MustCompile("`trello teach ([a-z-]+)`").FindStringSubmatch(line); topic != nil {
			surfaces = append(surfaces, h.run("teach", topic[1]).raw)
		}
	}
	for _, surface := range surfaces {
		if found := mention.FindString(surface); found != "" {
			t.Errorf("read-only output mentions %q:\n%.300s", found, surface)
		}
	}
	if !strings.Contains(h.run("describe").raw, `"writes_enabled":false`) {
		t.Error("describe must report writes_enabled false")
	}
	for _, name := range commands.MutatingNames {
		result := h.run(append(strings.Split(name, "."), "x", "--confirm")...)
		details, _ := result.doc["error"].(map[string]any)["details"].(map[string]any)
		if result.exit != 8 || details["reason"] != "writes_disabled" {
			t.Errorf("%s: exit %d %s", name, result.exit, result.raw)
		}
	}
	if h.fake.total() != 0 {
		t.Errorf("%d upstream requests", h.fake.total())
	}
}

func TestFullBuildOffersEveryMutatingCommand(t *testing.T) {
	if !commands.WritesEnabled {
		t.Skip("read-only build")
	}
	h := newHarness(t)
	listed := h.run("tools").raw
	for _, name := range commands.MutatingNames {
		if !strings.Contains(listed, `"`+name+`"`) {
			t.Errorf("tools lacks %s", name)
		}
	}
}

// A mistyped flag strands its value as a positional; the flag is what gets named (contract §3.3).
func TestUnknownFlagNamedBeforeStrayValue(t *testing.T) {
	h := newHarness(t)
	result := h.run("cards", "list", "--board", "B", "--limt", "3")
	details, _ := result.doc["error"].(map[string]any)["details"].(map[string]any)
	if result.exit != 2 || details["unknown_flag"] != "--limt" || details["did_you_mean"] != "--limit" {
		t.Errorf("got %s", result.raw)
	}
}

// Under a cap no record fits, each page still carries one record, so following next_cursor walks
// the whole listing instead of repeating one request (contract §8.4, C8.4-6).
func TestCursorProgressesWhenNoRecordFits(t *testing.T) {
	h := newHarness(t)
	h.fake.cards = 5
	seen := map[string]bool{}
	args := []string{"cards", "list", "--board", "B", "--max-bytes", "10"}
	for step := 0; step < 10; step++ {
		result := h.run(args...)
		records := result.data()
		if len(records) != 1 {
			t.Fatalf("step %d: %d records: %s", step, len(records), result.raw)
		}
		id := records[0].(map[string]any)["id"].(string)
		if seen[id] {
			t.Fatalf("step %d: %s repeated; the cursor did not move", step, id)
		}
		seen[id] = true
		page := result.page()
		if page["truncated_reason"] != "max_bytes_below_minimum" {
			t.Errorf("step %d: truncated_reason %v", step, page["truncated_reason"])
		}
		if page["has_more"] != true {
			break
		}
		args = []string{"cards", "list", "--cursor", page["next_cursor"].(string), "--max-bytes", "10"}
	}
	if len(seen) != 5 {
		t.Errorf("walked %d of 5 cards", len(seen))
	}
}

func TestArchiveAndDelete(t *testing.T) {
	if !commands.WritesEnabled {
		t.Skip("read-only build")
	}
	h := newHarness(t)
	for _, name := range []string{"archive", "delete"} {
		refused := h.run("cards", name, "c1")
		if refused.exit != 8 || refused.errorCode() != "refused" || h.fake.total() != 0 {
			t.Fatalf("%s without --confirm: exit %d, %d requests", name, refused.exit, h.fake.total())
		}
	}
	archived := h.run("cards", "archive", "c1", "--confirm")
	if archived.exit != 0 || h.fake.count("PUT /1/cards/c1") != 1 || !strings.Contains(h.fake.lastBody, `"closed":"true"`) {
		t.Fatalf("archive: exit %d, body %s", archived.exit, h.fake.lastBody)
	}
	deleted := h.run("cards", "delete", "c1", "--confirm")
	if deleted.exit != 0 || h.fake.count("DELETE /1/cards/c1") != 1 {
		t.Fatalf("delete: exit %d: %s", deleted.exit, deleted.raw)
	}
	missing := h.run("cards", "delete", "nope", "--confirm")
	if missing.errorCode() != "not_found" || h.fake.count("DELETE /1/cards/nope") != 1 {
		t.Errorf("deleting a missing card: %s", missing.raw)
	}
	described := h.run("describe", "cards.delete")
	annotations := described.doc["data"].(map[string]any)["commands"].([]any)[0].(map[string]any)["annotations"].(map[string]any)
	if annotations["destructiveHint"] != true || annotations["readOnlyHint"] != false {
		t.Errorf("delete annotations %v", annotations)
	}
}

func TestListProfiles(t *testing.T) {
	h := newHarness(t)
	empty := h.run("list-profiles")
	if empty.exit != 0 || empty.doc["data"] == nil || len(empty.data()) != 0 {
		t.Fatalf("no profiles: %s", empty.raw)
	}
	h.env["TRELLO_TIMEOUT_WORK"] = "5s"
	listed := h.run("list-profiles", "--profile", "work")
	records := listed.data()
	if len(records) != 2 || records[0].(map[string]any)["name"] != "default" || records[1].(map[string]any)["active"] != true {
		t.Errorf("got %s", listed.raw)
	}
	if strings.Contains(listed.raw, "5s") || strings.Contains(listed.raw, testToken) {
		t.Errorf("list-profiles must carry names only: %s", listed.raw)
	}
	failed := h.run("boards", "list", "--profile", "wrok")
	if failed.errorCode() != "config" || !strings.Contains(failed.raw, "list-profiles") {
		t.Errorf("an undeclared profile's hint should point to list-profiles: %s", failed.raw)
	}
	if h.fake.total() != 0 {
		t.Errorf("list-profiles contacted upstream %d times", h.fake.total())
	}
	for _, transport := range []Transport{TransportStdio, TransportHTTP} {
		if toolNames(h, transport)["list-profiles"] {
			t.Errorf("list-profiles must not be offered over %s", transport)
		}
	}
}

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestListProfilesEmitsDescriptionOnEveryEntry(t *testing.T) {
	h := newHarness(t)
	path := writeConfigFile(t, `{"default":{"description":"Day to day.","limit":9},"profiles":{"work":{"description":"Team boards."},"plain":{"limit":5}}}`)
	h.env["TRELLO_TIMEOUT_ENVONLY"] = "5s"
	listed := h.run("list-profiles", "--config", path, "--profile", "work")
	if listed.exit != 0 {
		t.Fatalf("exit %d: %s", listed.exit, listed.raw)
	}
	want := `"data":[` +
		`{"name":"default","active":false,"declared_in":[],"description":"Day to day."},` +
		`{"name":"envonly","active":false,"declared_in":["environment"],"description":null},` +
		`{"name":"plain","active":false,"declared_in":["config_file"],"description":null},` +
		`{"name":"work","active":true,"declared_in":["config_file"],"description":"Team boards."}],`
	if !strings.Contains(listed.raw, want) {
		t.Errorf("got %s\nwant it to contain %s", listed.raw, want)
	}
	if !strings.Contains(listed.raw, `"contract_version":"1.4.1"`) {
		t.Errorf("a tool that supports descriptions reports 1.4.1: %s", listed.raw)
	}
	if strings.Contains(listed.raw, "stripped_empty") {
		t.Errorf("list-profiles is not empty-stripped: %s", listed.raw)
	}

	kept := h.run("list-profiles", "--config", path, "--keep-empty")
	if !strings.Contains(kept.raw, `"name":"plain","active":false,"declared_in":["config_file"],"description":null`) {
		t.Errorf("--keep-empty changes nothing: %s", kept.raw)
	}

	human := h.run("list-profiles", "--config", path, "--human")
	if !strings.Contains(human.raw, "DESCRIPTION") || !strings.Contains(human.raw, "Team boards.") {
		t.Errorf("--human shows the description: %s", human.raw)
	}
}

func TestListProfilesWithNoneDeclaredStaysEmptyDespiteDefaultDescription(t *testing.T) {
	h := newHarness(t)
	path := writeConfigFile(t, `{"default":{"description":"Day to day."}}`)
	listed := h.run("list-profiles", "--config", path)
	if listed.exit != 0 || len(listed.data()) != 0 {
		t.Errorf("no declared profile means [] (contract §7.3 rule 1): %s", listed.raw)
	}
}

func TestDescriptionIsNotASettingAndIsValidatedEverywhere(t *testing.T) {
	h := newHarness(t)
	path := writeConfigFile(t, `{"default":{"description":"Day to day."},"profiles":{"work":{"description":"Team boards."}}}`)
	config := h.run("list-config", "--config", path, "--profile", "work")
	if config.exit != 0 || strings.Contains(strings.ToLower(config.raw), `"name":"description"`) {
		t.Errorf("list-config must not list description as a setting: %s", config.raw)
	}
	if strings.Contains(config.raw, "Team boards.") {
		t.Errorf("list-config must not carry the description: %s", config.raw)
	}
	schema := h.run("list-config", "--schema")
	if !strings.Contains(schema.raw, `"description":{"description":"One line saying what this profile is for.`) ||
		!strings.Contains(schema.raw, `"maxLength":200`) {
		t.Errorf("the config schema admits description (string, maxLength 200): %s", schema.raw)
	}
	bad := writeConfigFile(t, `{"profiles":{"work":{"description":"one\ntwo"}}}`)
	for _, args := range [][]string{{"list-config"}, {"list-profiles"}, {"doctor"}, {"boards", "list"}} {
		failed := h.run(append(args, "--config", bad)...)
		if failed.exit != 3 || failed.errorCode() != "config" || !strings.Contains(failed.raw, "profiles.work.description") {
			t.Errorf("%v: want config exit 3 naming profiles.work.description, got exit %d: %s", args, failed.exit, failed.raw)
		}
	}
}

func TestEnvelopeSchemaAcceptsPatchVersion(t *testing.T) {
	h := newHarness(t)
	described := h.run("describe")
	schema, _ := described.doc["data"].(map[string]any)["envelope_schema"].(map[string]any)
	meta := schema["properties"].(map[string]any)["meta"].(map[string]any)
	pattern := meta["properties"].(map[string]any)["contract_version"].(map[string]any)["pattern"].(string)
	matcher := regexp.MustCompile(pattern)
	for _, version := range []string{"1.4", "1.4.0", "1.4.1"} {
		if !matcher.MatchString(version) {
			t.Errorf("pattern %q rejects %s", pattern, version)
		}
	}
	for _, version := range []string{"1", "1.4.", "1.4.1.2", "v1.4"} {
		if matcher.MatchString(version) {
			t.Errorf("pattern %q accepts %s", pattern, version)
		}
	}
	if got := described.doc["data"].(map[string]any)["contract_version"]; got != "1.4.1" {
		t.Errorf("describe reports contract_version %v", got)
	}
}
