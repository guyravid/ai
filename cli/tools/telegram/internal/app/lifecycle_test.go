package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/guyravid/ai/cli/tools/telegram/internal/commands"
	"github.com/guyravid/ai/cli/tools/telegram/internal/registry"
	"github.com/guyravid/ai/cli/tools/telegram/internal/shape"
)

func TestInterruptedRunIsCanceled(t *testing.T) {
	for _, code := range []int{130, 143} {
		h := newHarness(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // the signal handler cancels the context before Run looks at the result
		var stdout bytes.Buffer
		exit := h.app.Run(ctx, []string{"updates", "list"}, &stdout, func() int { return code })
		if exit != code {
			t.Errorf("exit %d, want %d", exit, code)
		}
		if !strings.Contains(stdout.String(), `"code":"canceled"`) || strings.Count(stdout.String(), "\n") != 1 {
			t.Errorf("signal %d: %s", code, stdout.String())
		}
		if !strings.Contains(stdout.String(), `"command":"updates.list"`) {
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
	if result := h.run("bot", "get"); result.exit != 1 || result.errorCode() != "internal" || h.fake.total() != 0 {
		t.Errorf("exit %d after %d requests: %s", result.exit, h.fake.total(), result.raw)
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }

// mutatingMention matches a mutating command in either form, dotted or as a command line. With no
// mutating command yet it matches nothing.
func mutatingMention() *regexp.Regexp {
	var alternatives []string
	for _, name := range commands.MutatingNames {
		group, verb, _ := strings.Cut(name, ".")
		alternatives = append(alternatives, regexp.QuoteMeta(group)+`[. ]`+regexp.QuoteMeta(verb)+`\b`)
	}
	if len(alternatives) == 0 {
		return regexp.MustCompile(`^\b$`)
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
		if topic := regexp.MustCompile("`telegram teach ([a-z-]+)`").FindStringSubmatch(line); topic != nil {
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
		result := h.run(append(strings.Split(name, "."), "--confirm")...)
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
	result := h.run("updates", "list", "--limt", "3")
	details, _ := result.doc["error"].(map[string]any)["details"].(map[string]any)
	if result.exit != 2 || details["unknown_flag"] != "--limt" || details["did_you_mean"] != "--limit" {
		t.Errorf("got %s", result.raw)
	}
}

func TestListProfiles(t *testing.T) {
	h := newHarness(t)
	empty := h.run("list-profiles")
	if empty.exit != 0 || empty.doc["data"] == nil || len(empty.data()) != 0 {
		t.Fatalf("no profiles: %s", empty.raw)
	}
	h.env["TELEGRAM_TIMEOUT_WORK"] = "5s"
	listed := h.run("list-profiles", "--profile", "work")
	records := listed.data()
	if len(records) != 2 || records[0].(map[string]any)["name"] != "default" || records[1].(map[string]any)["active"] != true {
		t.Errorf("got %s", listed.raw)
	}
	if strings.Contains(listed.raw, "5s") || strings.Contains(listed.raw, testToken) {
		t.Errorf("list-profiles must carry names only: %s", listed.raw)
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

func TestListProfilesEmitsDescriptionOnEveryEntry(t *testing.T) {
	h := newHarness(t)
	path := writeConfigFile(t, `{"default":{"description":"Day to day.","limit":9},"profiles":{"work":{"description":"Team bot."},"plain":{"limit":5}}}`)
	h.env["TELEGRAM_TIMEOUT_ENVONLY"] = "5s"
	listed := h.run("list-profiles", "--config", path, "--profile", "work")
	if listed.exit != 0 {
		t.Fatalf("exit %d: %s", listed.exit, listed.raw)
	}
	want := `"data":[` +
		`{"name":"default","active":false,"declared_in":[],"description":"Day to day."},` +
		`{"name":"envonly","active":false,"declared_in":["environment"],"description":null},` +
		`{"name":"plain","active":false,"declared_in":["config_file"],"description":null},` +
		`{"name":"work","active":true,"declared_in":["config_file"],"description":"Team bot."}],`
	if !strings.Contains(listed.raw, want) {
		t.Errorf("got %s\nwant it to contain %s", listed.raw, want)
	}
	if strings.Contains(listed.raw, "stripped_empty") {
		t.Errorf("list-profiles is not empty-stripped: %s", listed.raw)
	}
	kept := h.run("list-profiles", "--config", path, "--keep-empty")
	if !strings.Contains(kept.raw, `"name":"plain","active":false,"declared_in":["config_file"],"description":null`) {
		t.Errorf("--keep-empty changes nothing: %s", kept.raw)
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
	path := writeConfigFile(t, `{"default":{"description":"Day to day."},"profiles":{"work":{"description":"Team bot."}}}`)
	config := h.run("list-config", "--config", path, "--profile", "work")
	if config.exit != 0 || strings.Contains(strings.ToLower(config.raw), `"name":"description"`) || strings.Contains(config.raw, "Team bot.") {
		t.Errorf("list-config must not list description as a setting or carry it: %s", config.raw)
	}
	schema := h.run("list-config", "--schema")
	if !strings.Contains(schema.raw, `"maxLength":200`) || !strings.Contains(schema.raw, `"default_chat"`) ||
		!strings.Contains(schema.raw, `"bot_token_file"`) || strings.Contains(schema.raw, `"bot_token":`) {
		t.Errorf("the config schema admits description, the new settings and bot_token_file only: %s", schema.raw)
	}
	bad := writeConfigFile(t, `{"profiles":{"work":{"description":"one\ntwo"}}}`)
	for _, args := range [][]string{{"list-config"}, {"list-profiles"}, {"doctor"}, {"bot", "get"}} {
		failed := h.run(append(args, "--config", bad)...)
		if failed.exit != 3 || failed.errorCode() != "config" || !strings.Contains(failed.raw, "profiles.work.description") {
			t.Errorf("%v: want config exit 3 naming profiles.work.description, got exit %d: %s", args, failed.exit, failed.raw)
		}
	}
}

func TestConfigFileRejectsACredentialValue(t *testing.T) {
	h := newHarness(t)
	path := writeConfigFile(t, `{"default":{"bot_token":"`+testToken+`"}}`)
	failed := h.run("list-config", "--config", path)
	if failed.exit != 3 || strings.Contains(failed.raw, testToken) {
		t.Errorf("exit %d: %s", failed.exit, failed.raw)
	}
}

func TestEnvelopeSchemaAcceptsPatchVersion(t *testing.T) {
	h := newHarness(t)
	described := h.run("describe")
	schema, _ := described.doc["data"].(map[string]any)["envelope_schema"].(map[string]any)
	meta := schema["properties"].(map[string]any)["meta"].(map[string]any)
	pattern := meta["properties"].(map[string]any)["contract_version"].(map[string]any)["pattern"].(string)
	matcher := regexp.MustCompile(pattern)
	for _, version := range []string{"1.4", "1.4.0", "1.4.1", "1.4.2"} {
		if !matcher.MatchString(version) {
			t.Errorf("pattern %q rejects %s", pattern, version)
		}
	}
	for _, version := range []string{"1", "1.4.", "1.4.1.2", "v1.4"} {
		if matcher.MatchString(version) {
			t.Errorf("pattern %q accepts %s", pattern, version)
		}
	}
	if got := described.doc["data"].(map[string]any)["contract_version"]; got != "1.4.2" {
		t.Errorf("describe reports contract_version %v", got)
	}
}

// Orientation and topics may name only commands this build has (contract §6.3).
func TestTeachNamesOnlyCommandsThatExist(t *testing.T) {
	h := newHarness(t)
	surfaces := map[string]string{"teach": h.run("teach").raw, "contract": h.run("teach", "contract").raw,
		"flags": h.run("teach", "flags").raw, "config": h.run("teach", "config").raw}
	for _, line := range strings.Split(h.run("teach", "--list").raw, "\n") {
		if topic := regexp.MustCompile("`telegram teach ([a-z-]+)`").FindStringSubmatch(line); topic != nil {
			surfaces[topic[1]] = h.run("teach", topic[1]).raw
		}
	}
	topics := []string{"chats", "updates", "profiles"}
	if commands.WritesEnabled {
		topics = append(topics, "delegation", "uploads")
	}
	for _, topic := range topics {
		if surfaces[topic] == "" {
			t.Errorf("teach topic %s is missing", topic)
		}
	}
	if !commands.WritesEnabled {
		for _, topic := range []string{"delegation", "uploads"} {
			if surfaces[topic] != "" {
				t.Errorf("the read-only build has no %s topic: it teaches writes", topic)
			}
		}
	}
	argvs := map[string]bool{}
	for _, command := range h.app.Registry.Commands() {
		argvs[strings.Join(command.Argv(), " ")] = true
	}
	mention := regexp.MustCompile("`telegram ((?:[a-z][a-z-]*)(?: [a-z][a-z-]*)?)")
	for name, text := range surfaces {
		for _, match := range mention.FindAllStringSubmatch(text, -1) {
			words := strings.Fields(match[1])
			if argvs[strings.Join(words, " ")] || argvs[words[0]] {
				continue
			}
			t.Errorf("teach %s names `telegram %s`, which is not a command of this build", name, match[1])
		}
	}
}

func TestOrientationStatesWhatThisBuildCanDo(t *testing.T) {
	h := newHarness(t)
	text := h.run("teach").raw
	if len(text) >= 4096 {
		t.Errorf("orientation is %d bytes, the contract's soft limit is 4096", len(text))
	}
	for _, want := range []string{"PR-assistant bot", "list-profiles", "--profile", "telegram bot get", "telegram updates list", "chat_unreachable"} {
		if !strings.Contains(text, want) {
			t.Errorf("orientation lacks %q:\n%s", want, text)
		}
	}
	// What is said about sending, waiting and acknowledging appears only when the commands exist.
	for command, phrase := range map[string]string{"messages.send": "--confirm", "updates.wait": "updates wait", "updates.ack": "updates ack"} {
		if exists := h.app.Registry.Get(command) != nil; exists != strings.Contains(text, phrase) {
			t.Errorf("%s exists=%v but orientation mentions %q = %v", command, exists, phrase, strings.Contains(text, phrase))
		}
	}
}

func TestTeachConfigRecordsTheTokenIsolationRule(t *testing.T) {
	h := newHarness(t)
	credentials := h.run("teach", "config", "credentials").raw
	profiles := h.run("teach", "config", "profiles").raw
	profilesTopic := h.run("teach", "profiles").raw
	for name, text := range map[string]string{"config credentials": credentials, "config profiles": profiles, "profiles": profilesTopic} {
		if !strings.Contains(text, "never from the default scope") && !strings.Contains(text, "default bot") {
			t.Errorf("teach %s should state that a profile never uses the default scope's token", name)
		}
	}
	for _, want := range []string{"TELEGRAM_BOT_TOKEN_<PROFILE>", "skipped", "exit 3", "even when `TELEGRAM_BOT_TOKEN_FILE` is set", "`BOT_TOKEN` is a profile-scoped credential"} {
		if !strings.Contains(credentials, want) {
			t.Errorf("teach config credentials lacks %q", want)
		}
	}
	if !strings.Contains(profiles, "default_chat") || !strings.Contains(profiles, "allowed_chats") || !strings.Contains(profiles, "Start") {
		t.Errorf("teach config profiles should say what a profile inherits (trap 7): %s", profiles)
	}
	example := h.run("teach", "config", "example").raw
	if !strings.Contains(example, `"bot_token_file"`) || !strings.Contains(example, `"description"`) {
		t.Errorf("example config: %s", example)
	}
}

// The reserved flags that apply to no command here are not offered.
func TestFlagsOfRemovedFeaturesAreAbsent(t *testing.T) {
	h := newHarness(t)
	flags := h.run("teach", "flags").raw
	for _, gone := range []string{"`--all`", "`--offset"} {
		if strings.Contains(flags, gone) {
			t.Errorf("teach flags lists %s", gone)
		}
	}
	for _, kept := range []string{"`--dataset-dir", "`--limit", "`--since"} {
		if !strings.Contains(flags, kept) {
			t.Errorf("teach flags lacks %s", kept)
		}
	}
}

func TestWriteFilesNeedOwnerOnlyTokenFiles(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "TELEGRAM_BOT_TOKEN")
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(testToken), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	h.env["TELEGRAM_BOT_TOKEN_FILE"] = path
	result := h.run("bot", "get")
	if result.exit != 3 || !strings.Contains(fmt.Sprint(result.errorBody()["hint"]), "chmod 600") || h.fake.total() != 0 {
		t.Errorf("exit %d: %s", result.exit, result.raw)
	}
}
