package app

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/guyravid/ai/cli/tools/telegram/internal/commands"
)

func toolNames(h *harness, transport Transport) map[string]bool {
	names := map[string]bool{}
	for _, command := range h.app.ToolCommands(transport) {
		names[command.Name] = true
	}
	return names
}

func TestToolCommandsPerTransport(t *testing.T) {
	h := newHarness(t)
	stdio, remote := toolNames(h, TransportStdio), toolNames(h, TransportHTTP)
	for _, name := range []string{"tools", "describe", "serve"} {
		if stdio[name] || remote[name] {
			t.Errorf("%s must not be an MCP tool", name)
		}
	}
	if !stdio["teach"] || !remote["teach"] {
		t.Error("teach must be offered on both transports")
	}
	for _, name := range []string{"doctor", "list-config"} {
		if !stdio[name] {
			t.Errorf("%s should be offered over stdio", name)
		}
		if remote[name] {
			t.Errorf("%s must not be offered over HTTP", name)
		}
	}
	for _, name := range []string{"bot.get", "chats.get", "updates.list", "version"} {
		if !remote[name] {
			t.Errorf("%s should be offered over HTTP", name)
		}
	}
	for _, name := range commands.MutatingNames {
		if stdio[name] != commands.WritesEnabled {
			t.Errorf("%s offered=%v, writes enabled=%v", name, stdio[name], commands.WritesEnabled)
		}
	}
}

// The tool schema carries describe's parameters unchanged; the only additions are per-call flags.
func TestToolInputSchemaExtendsDescribe(t *testing.T) {
	h := newHarness(t)
	listProperties := len(h.app.Registry.Get("updates.list").InputSchema.Properties)
	for _, command := range h.app.ToolCommands(TransportStdio) {
		schema := ToolInputSchema(command)
		for name, property := range command.InputSchema.Properties {
			if !reflect.DeepEqual(schema.Properties[name], property) {
				t.Errorf("%s.%s differs from describe", command.Name, name)
			}
		}
		if !reflect.DeepEqual(schema.Required, command.InputSchema.Required) {
			t.Errorf("%s required differs from describe", command.Name)
		}
		extras := map[string]bool{}
		for _, flag := range perCallFlags(command) {
			extras[ArgumentName(flag)] = true
		}
		for name := range schema.Properties {
			if command.InputSchema.Properties[name] == nil && !extras[name] {
				t.Errorf("%s has unexpected argument %s", command.Name, name)
			}
		}
		_, confirm := schema.Properties["confirm"]
		if confirm != command.IsWrite() {
			t.Errorf("%s confirm argument present=%v", command.Name, confirm)
		}
		if command.IsWrite() && schema.Properties["dry_run"] == nil {
			t.Errorf("%s lacks dry_run", command.Name)
		}
	}
	if len(newHarness(t).app.Registry.Get("updates.list").InputSchema.Properties) != listProperties {
		t.Error("ToolInputSchema must not modify the registry's schema")
	}
}

func TestToolOutputSchemaWrapsData(t *testing.T) {
	h := newHarness(t)
	if ToolOutputSchema(h.app.Registry.Get("teach")) != nil {
		t.Error("teach returns Markdown and has no output schema")
	}
	command := h.app.Registry.Get("updates.list")
	schema := ToolOutputSchema(command)
	got, _ := json.Marshal(schema.Properties["data"].AnyOf[0])
	want, _ := json.Marshal(command.OutputSchema)
	if string(got) != string(want) {
		t.Error("data must be the command's outputSchema")
	}
	if null := schema.Properties["data"].AnyOf[1]; null.Type != "null" {
		t.Error("data must also admit null, for failures")
	}
	if schema.Properties["meta"] == nil || schema.ID != "" {
		t.Error("the rest of the envelope schema must be kept, without its $id")
	}
}

func TestBindArguments(t *testing.T) {
	h := newHarness(t)
	command := h.app.Registry.Get("updates.list")
	server := map[string]string{"profile": "work"}
	invocation, err := BindArguments(command,
		json.RawMessage(`{"chat":"111","limit":5,"max_bytes":1000000,"fields":"update_id,text","keep_empty":true,"dry_run":false}`), server)
	if err != nil {
		t.Fatal(err)
	}
	if invocation.Params["chat"] != "111" || len(invocation.Params) != 1 {
		t.Errorf("params = %v", invocation.Params)
	}
	want := map[string]string{"profile": "work", "limit": "5", "max-bytes": "1000000", "fields": "update_id,text", "keep-empty": "true"}
	if !reflect.DeepEqual(invocation.Flags, want) {
		t.Errorf("flags = %v, want %v", invocation.Flags, want)
	}

	for arguments, detail := range map[string]string{
		`{"list":"x"}`:     "list",    // teach's flag is not a per-call argument here
		`{"pretty":true}`:  "pretty",  // output formatting is not per call
		`{"limit":true}`:   "limit",   // wrong type
		`{"all":true}`:     "all",     // removed with the datasets
		`{"offset":3}`:     "offset",  // removed with the datasets
		`{"profile":"x"}`:  "profile", // server-level only
		`{"max-bytes":10}`: "max-bytes",
	} {
		if _, err := BindArguments(command, json.RawMessage(arguments), nil); err == nil || err.Code != "usage" {
			t.Errorf("%s (%s): got %v, want usage", arguments, detail, err)
		}
	}
	// allow_any_chat is an argument of the full build only. The read-only build refuses it for the
	// right reason, as it refuses the flag on the command line.
	anyChat, err := BindArguments(command, json.RawMessage(`{"allow_any_chat":true}`), nil)
	if commands.WritesEnabled {
		if err != nil || anyChat.Params["allow_any_chat"] != true {
			t.Errorf("full build: %v %v", anyChat, err)
		}
	} else if err == nil || err.Code != "refused" || err.Details["reason"] != "any_chat_requires_write_build" {
		t.Errorf("read-only build: got %v, want refused any_chat_requires_write_build", err)
	}
	if _, err := BindArguments(command, json.RawMessage(`[1]`), nil); err == nil {
		t.Error("a non-object must be refused")
	}
	if invocation, err := BindArguments(h.app.Registry.Get("version"), nil, nil); err != nil || len(invocation.Params) != 0 {
		t.Errorf("empty arguments: %v %v", invocation, err)
	}
}

func executeTool(t *testing.T, h *harness, name, arguments string, transport Transport) map[string]any {
	t.Helper()
	response := h.app.ExecuteTool(context.Background(), ToolCall{
		Command: h.app.Registry.Get(name), Arguments: json.RawMessage(arguments), Transport: transport,
	})
	defer response.Cleanup()
	var document map[string]any
	if err := json.Unmarshal(response.Document(), &document); err != nil {
		t.Fatalf("%s: %v\n%s", name, err, response.Document())
	}
	return document
}

func TestExecuteToolMatchesCommandLine(t *testing.T) {
	h := newHarness(t)
	h.fake.updates = []string{message(1, 111, time.Hour, "a"), message(2, 111, time.Hour, "b")}
	viaTool := h.app.ExecuteTool(context.Background(), ToolCall{
		Command: h.app.Registry.Get("updates.list"), Arguments: json.RawMessage(`{"limit":3,"deterministic":true}`),
		Transport: TransportStdio,
	})
	viaArgv := h.run("updates", "list", "--limit", "3", "--deterministic")
	if string(viaTool.Document()) != viaArgv.raw {
		t.Errorf("tool and argv differ:\n%s\n%s", viaTool.Document(), viaArgv.raw)
	}
}

func TestExecuteToolUnknownArgumentIsUsage(t *testing.T) {
	h := newHarness(t)
	document := executeTool(t, h, "updates.list", `{"bogus":1}`, TransportStdio)
	if document["ok"] != false || document["error"].(map[string]any)["code"] != "usage" || h.fake.total() != 0 {
		t.Errorf("got %v after %d requests", document, h.fake.total())
	}
}

func TestExecuteToolIntegerParameter(t *testing.T) {
	h := newHarness(t)
	h.fake.updates = []string{message(1, 111, time.Hour, "a"), message(2, 111, time.Hour, "b")}
	document := executeTool(t, h, "updates.list", `{"after":1,"fields":"update_id"}`, TransportStdio)
	data, _ := document["data"].([]any)
	if document["ok"] != true || len(data) != 1 || data[0].(map[string]any)["update_id"] != float64(2) {
		t.Errorf("got %v", document)
	}
}

func TestExecuteToolRecoversPanics(t *testing.T) {
	h := newHarness(t)
	response := h.app.ExecuteTool(context.Background(), ToolCall{Command: nil})
	if !strings.Contains(string(response.Document()), `"code":"internal"`) {
		t.Errorf("a crash must be internal: %s", response.Document())
	}
}

// Shaped responses, and failures, validate against the full output schema (contract §6.2, C6-18,
// C16-9). Before 1.2, --fields, empty-stripping, and --max-depth all produced violations.
// Shaped responses, and failures, validate against the full output schema (contract §6.2).
func TestShapedResponsesMatchFullOutputSchema(t *testing.T) {
	h := newHarness(t)
	h.env["TELEGRAM_DEFAULT_CHAT"] = "111111111"
	h.fake.updates = []string{message(1, 111111111, time.Hour, "a"), message(2, 111111111, time.Hour, strings.Repeat("long ", 100))}
	cases := []struct {
		command string
		args    []string
	}{
		{"updates.list", []string{"updates", "list"}},
		{"updates.list", []string{"updates", "list", "--fields", "update_id,chat.id"}},
		{"updates.list", []string{"updates", "list", "--fields", "*", "--max-depth", "1"}},
		{"updates.list", []string{"updates", "list", "--max-string", "5", "--fields", "update_id,text"}},
		{"updates.list", []string{"updates", "list", "--limit", "1", "--max-bytes", "400"}},
		{"updates.list", []string{"updates", "list", "--chat", "999999999"}},
		{"bot.get", []string{"bot", "get"}},
		{"bot.get", []string{"bot", "get", "--fields", "*", "--max-depth", "1"}},
		{"chats.get", []string{"chats", "get"}},
		{"chats.get", []string{"chats", "get", "--chat", "424242"}}, // a failure: data is null
	}
	for _, tc := range cases {
		resolved, err := ToolOutputSchema(h.app.Registry.Get(tc.command)).Resolve(nil)
		if err != nil {
			t.Fatalf("%s: resolve: %v", tc.command, err)
		}
		result := h.run(tc.args...)
		var instance any
		if err := json.Unmarshal([]byte(result.raw), &instance); err != nil {
			t.Fatal(err)
		}
		if err := resolved.Validate(instance); err != nil {
			t.Errorf("%v: %v\n%.400s", tc.args, err, result.raw)
		}
	}
}

// The validation above is not vacuous: a malformed envelope fails, and so does a projected response
// against a strict schema that requires a field the projection dropped.
func TestOutputSchemaValidationRejects(t *testing.T) {
	h := newHarness(t)
	h.fake.updates = []string{message(1, 111, time.Hour, "a")}
	command := h.app.Registry.Get("updates.list")
	resolved, err := ToolOutputSchema(command).Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Validate(map[string]any{"ok": "yes"}) == nil {
		t.Error("a malformed envelope validated")
	}
	strict := ToolOutputSchema(command)
	open := strict.Properties["data"].AnyOf[0]
	record := *open.Items
	record.Required = []string{"update_id", "text"}
	strictData := *open
	strictData.Items = &record
	strict.Properties["data"] = &strictData
	strictResolved, err := strict.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	var instance any
	_ = json.Unmarshal([]byte(h.run("updates", "list", "--fields", "update_id").raw), &instance)
	if strictResolved.Validate(instance) == nil {
		t.Error("a projected response validated against a schema requiring text")
	}
	if err := resolved.Validate(instance); err != nil {
		t.Errorf("the open schema must accept it: %v", err)
	}
}

// A mutating tool takes confirm and dry_run and behaves as §11.2: without confirm it is refused with
// a preview, with dry_run it sends nothing even when confirmed, and with confirm it sends once.
func TestExecuteToolWritesHonourConfirmAndDryRun(t *testing.T) {
	h := writesHarness(t)
	for _, name := range []string{"messages.send", "messages.send-document", "messages.send-photo", "updates.ack"} {
		schema := ToolInputSchema(h.app.Registry.Get(name))
		if schema.Properties["confirm"] == nil || schema.Properties["dry_run"] == nil {
			t.Errorf("%s lacks confirm or dry_run", name)
		}
	}

	refused := executeTool(t, h, "messages.send", `{"text":"hello"}`, TransportStdio)
	body, _ := refused["error"].(map[string]any)
	details, _ := body["details"].(map[string]any)
	if refused["ok"] != false || body["code"] != "refused" || details["preview"] == nil || h.fake.total() != 0 {
		t.Fatalf("without confirm: %v after %d requests", refused, h.fake.total())
	}

	dry := executeTool(t, h, "messages.send", `{"text":"hello","confirm":true,"dry_run":true}`, TransportStdio)
	if dry["ok"] != true || h.fake.total() != 0 {
		t.Fatalf("dry_run: %v after %d requests", dry, h.fake.total())
	}

	sent := executeTool(t, h, "messages.send", `{"text":"hello","confirm":true}`, TransportStdio)
	if sent["ok"] != true || h.fake.count("sendMessage") != 1 || h.fake.total() != 1 {
		t.Fatalf("confirm: %v after %d requests", sent, h.fake.total())
	}

	ack := executeTool(t, h, "updates.ack", `{"through":5}`, TransportStdio)
	if ack["ok"] != false || h.fake.count("getUpdates") != 0 {
		t.Errorf("updates.ack without confirm must be refused and send nothing: %v", ack)
	}
}

// Arguments that name a flag of one tool but not another are usage, and a per-call flag reaches the
// command: limit shapes the result the same way --limit does.
func TestExecuteToolPerCallFlagsReachTheCommand(t *testing.T) {
	h := newHarness(t)
	h.fake.updates = []string{message(1, 111, time.Hour, "a"), message(2, 111, time.Hour, "b"), message(3, 111, time.Hour, "c")}
	document := executeTool(t, h, "updates.list", `{"limit":2,"fields":"update_id"}`, TransportStdio)
	if data, _ := document["data"].([]any); len(data) != 2 {
		t.Errorf("limit not applied: %v", document)
	}
}
