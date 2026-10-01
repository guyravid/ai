package app

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/guyravid/ai/cli/tools/trello/internal/commands"
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
	for _, name := range []string{"dataset.clear", "dataset.rm", "doctor", "list-config"} {
		if !stdio[name] {
			t.Errorf("%s should be offered over stdio", name)
		}
		if remote[name] {
			t.Errorf("%s must not be offered over HTTP", name)
		}
	}
	for _, name := range []string{"dataset.read", "dataset.list", "dataset.stat", "cards.list"} {
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
		if confirm != (command.IsWrite() || command.Name == "dataset.clear") {
			t.Errorf("%s confirm argument present=%v", command.Name, confirm)
		}
		if command.IsWrite() && schema.Properties["dry_run"] == nil {
			t.Errorf("%s lacks dry_run", command.Name)
		}
	}
	if len(newHarness(t).app.Registry.Get("cards.list").InputSchema.Properties) != 2 {
		t.Error("ToolInputSchema must not modify the registry's schema")
	}
}

func TestToolOutputSchemaWrapsData(t *testing.T) {
	h := newHarness(t)
	if ToolOutputSchema(h.app.Registry.Get("teach")) != nil {
		t.Error("teach returns Markdown and has no output schema")
	}
	command := h.app.Registry.Get("cards.list")
	schema := ToolOutputSchema(command)
	got, _ := json.Marshal(schema.Properties["data"])
	want, _ := json.Marshal(command.OutputSchema)
	if string(got) != string(want) {
		t.Error("data must be the command's outputSchema")
	}
	if schema.Properties["meta"] == nil || schema.ID != "" {
		t.Error("the rest of the envelope schema must be kept, without its $id")
	}
}

func TestBindArguments(t *testing.T) {
	h := newHarness(t)
	command := h.app.Registry.Get("cards.list")
	server := map[string]string{"profile": "work"}
	invocation, err := BindArguments(command,
		json.RawMessage(`{"board":"B","limit":5,"max_bytes":1000000,"fields":"id,name","keep_empty":true,"dry_run":false}`), server)
	if err != nil {
		t.Fatal(err)
	}
	if invocation.Params["board"] != "B" || len(invocation.Params) != 1 {
		t.Errorf("params = %v", invocation.Params)
	}
	want := map[string]string{"profile": "work", "limit": "5", "max-bytes": "1000000", "fields": "id,name", "keep-empty": "true"}
	if !reflect.DeepEqual(invocation.Flags, want) {
		t.Errorf("flags = %v, want %v", invocation.Flags, want)
	}

	for arguments, detail := range map[string]string{
		`{"board":"B","list":"x"}`:     "list",    // teach's flag is not a per-call argument here
		`{"board":"B","pretty":true}`:  "pretty",  // output formatting is not per call
		`{"board":"B","limit":true}`:   "limit",   // wrong type
		`{"board":"B","all":"yes"}`:    "all",     // wrong type
		`{"board":"B","profile":"x"}`:  "profile", // server-level only
		`{"board":"B","max-bytes":10}`: "max-bytes",
	} {
		if _, err := BindArguments(command, json.RawMessage(arguments), nil); err == nil || err.Code != "usage" {
			t.Errorf("%s (%s): got %v, want usage", arguments, detail, err)
		}
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
	viaTool := h.app.ExecuteTool(context.Background(), ToolCall{
		Command: h.app.Registry.Get("cards.list"), Arguments: json.RawMessage(`{"board":"B","limit":3,"deterministic":true}`),
		Transport: TransportStdio,
	})
	viaArgv := h.run("cards", "list", "--board", "B", "--limit", "3", "--deterministic")
	if string(viaTool.Document()) != viaArgv.raw {
		t.Errorf("tool and argv differ:\n%s\n%s", viaTool.Document(), viaArgv.raw)
	}
}

func TestExecuteToolUnknownArgumentIsUsage(t *testing.T) {
	h := newHarness(t)
	document := executeTool(t, h, "cards.list", `{"board":"B","bogus":1}`, TransportStdio)
	if document["ok"] != false || document["error"].(map[string]any)["code"] != "usage" || h.fake.total() != 0 {
		t.Errorf("got %v after %d requests", document, h.fake.total())
	}
}

func TestExecuteToolWriteNeedsConfirm(t *testing.T) {
	if !commands.WritesEnabled {
		t.Skip("read-only build")
	}
	h := newHarness(t)
	arguments := `{"list_id":"L1","name":"Fix paging"}`
	refused := executeTool(t, h, "cards.create", arguments, TransportStdio)
	if refused["ok"] != false || h.fake.total() != 0 {
		t.Fatalf("write without confirm: %v, %d requests", refused, h.fake.total())
	}
	dryRun := executeTool(t, h, "cards.create", `{"list_id":"L1","name":"Fix paging","confirm":true,"dry_run":true}`, TransportStdio)
	if dryRun["ok"] != true || h.fake.total() != 0 {
		t.Fatalf("dry run: %v, %d requests", dryRun, h.fake.total())
	}
	sent := executeTool(t, h, "cards.create", `{"list_id":"L1","name":"Fix paging","confirm":true}`, TransportStdio)
	if sent["ok"] != true || h.fake.count("POST /1/cards") != 1 {
		t.Fatalf("confirmed write: %v", sent)
	}
}

func TestExecuteToolHidesDatasetPathOverHTTP(t *testing.T) {
	h := newHarness(t)
	datasetMeta := func(document map[string]any) map[string]any {
		meta, _ := document["meta"].(map[string]any)
		dataset, _ := meta["dataset"].(map[string]any)
		return dataset
	}
	local := datasetMeta(executeTool(t, h, "cards.list", `{"board":"B","all":true}`, TransportStdio))
	if path, _ := local["path"].(string); path == "" {
		t.Fatalf("stdio should report the dataset path: %v", local)
	}
	remote := datasetMeta(executeTool(t, h, "cards.list", `{"board":"B","all":true}`, TransportHTTP))
	if remote == nil || remote["id"] == nil {
		t.Fatalf("HTTP should still report the dataset: %v", remote)
	}
	if _, present := remote["path"]; present {
		t.Errorf("HTTP must omit meta.dataset.path: %v", remote)
	}
}

func TestExecuteToolRecoversPanics(t *testing.T) {
	h := newHarness(t)
	response := h.app.ExecuteTool(context.Background(), ToolCall{Command: nil})
	if !strings.Contains(string(response.Document()), `"code":"internal"`) {
		t.Errorf("a crash must be internal: %s", response.Document())
	}
}
