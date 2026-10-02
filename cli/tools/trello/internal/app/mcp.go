package app

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
	"github.com/guyravid/ai/cli/tools/trello/internal/registry"
)

// This file is the transport-neutral half of server mode (contract §16.2): which commands become
// tools, their schemas, and how tool arguments bind onto the same Invocation argv parsing builds.
// It links no MCP code, so it is tested in every build.

// Transport is how the server is reached; it decides which commands are offered.
type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportHTTP  Transport = "http"
)

// notOverMCP are replaced by the protocol's own tool listing, are the server itself, or (list-profiles)
// are of no use when the profile is fixed at server start (contract §16.2).
var notOverMCP = map[string]bool{"tools": true, "describe": true, "serve": true, "list-profiles": true}

// notOverHTTP act on the server's own filesystem or configuration, which a remote client does not
// share (contract §16.3; doctor and list-config by operator decision).
var notOverHTTP = map[string]bool{"dataset.clear": true, "dataset.rm": true, "doctor": true, "list-config": true}

// ToolCommands returns the commands offered as MCP tools over a transport, in registry order. A
// read-only build has no mutating commands in its registry, so none are offered.
func (a *App) ToolCommands(transport Transport) []*registry.Command {
	var offered []*registry.Command
	for _, command := range a.Registry.Commands() {
		if notOverMCP[command.Name] || (transport == TransportHTTP && notOverHTTP[command.Name]) {
			continue
		}
		offered = append(offered, command)
	}
	return offered
}

// ArgumentName is the tool argument for a reserved flag: no dashes, - becomes _ (§16.2).
func ArgumentName(flag *registry.FlagDef) string { return strings.ReplaceAll(flag.Name, "-", "_") }

// perCallFlags are the reserved flags a command accepts as tool arguments.
func perCallFlags(command *registry.Command) []*registry.FlagDef {
	var flags []*registry.FlagDef
	for index := range registry.ReservedFlags {
		flag := &registry.ReservedFlags[index]
		if flag.MCPExtra && registry.FlagApplies(flag, command) {
			flags = append(flags, flag)
		}
	}
	return flags
}

// integerFlags take a whole number; every other value-taking flag takes a string.
var integerFlags = map[string]bool{
	"limit": true, "max-bytes": true, "max-string": true, "max-depth": true, "max-pages": true, "offset": true,
}

// ToolInputSchema is the command's describe inputSchema plus its per-call flags as arguments. The
// command's own parameters are carried over unchanged, so they match describe exactly.
func ToolInputSchema(command *registry.Command) *jsonschema.Schema {
	// A shallow copy: nested schemas are shared, read-only, so property order survives.
	copied := *command.InputSchema
	schema := &copied
	schema.Properties = maps.Clone(command.InputSchema.Properties)
	if schema.Properties == nil {
		schema.Properties = map[string]*jsonschema.Schema{}
	}
	schema.PropertyOrder = slices.Clone(command.InputSchema.PropertyOrder)
	for _, flag := range perCallFlags(command) {
		property := &jsonschema.Schema{Type: "string", Description: flag.Meaning}
		switch {
		case flag.Value == "":
			property.Type = "boolean"
		case integerFlags[flag.Name]:
			property.Type = "integer"
		}
		schema.Properties[ArgumentName(flag)] = property
		schema.PropertyOrder = append(schema.PropertyOrder, ArgumentName(flag))
	}
	return schema
}

// ToolOutputSchema is the command's full output schema (contract §6.2, §16.2): the envelope schema
// with data replaced by anyOf the command's outputSchema and null, since a failure carries
// data:null. teach returns Markdown, not an envelope, and has none.
func ToolOutputSchema(command *registry.Command) *jsonschema.Schema {
	if command.Name == "teach" {
		return nil
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal([]byte(EnvelopeSchema), &schema); err != nil {
		panic(err)
	}
	schema.ID = "" // no longer the shared envelope schema
	schema.Properties["data"] = &jsonschema.Schema{AnyOf: []*jsonschema.Schema{command.OutputSchema, {Type: "null"}}}
	return &schema
}

// ToolCall is one MCP tool call, before binding.
type ToolCall struct {
	Command   *registry.Command
	Arguments json.RawMessage
	// ServerFlags are the serve flags every call inherits (profile, config, deterministic, verbose).
	ServerFlags map[string]string
	Transport   Transport
}

// ExecuteTool binds a tool call onto an Invocation and runs it through Execute, the same path as
// the command line. The response never carries a dataset path over HTTP.
func (a *App) ExecuteTool(ctx context.Context, call ToolCall) (response *Response) {
	defer func() {
		if recovered := recover(); recovered != nil {
			var command *string
			if call.Command != nil {
				command = &call.Command.Name
			}
			response = a.errorResponse(command, errs.New(errs.Internal, "The tool failed unexpectedly: %v.", recovered).
				WithHint("Report this with --verbose output."), false)
		}
	}()
	name := call.Command.Name
	if a.StartupError != nil {
		return a.errorResponse(&name, errs.New(errs.Internal, "The command registry is invalid: %s.", a.StartupError.Error()), false)
	}
	invocation, err := BindArguments(call.Command, call.Arguments, call.ServerFlags)
	if err != nil {
		return a.errorResponse(&name, err, false)
	}
	invocation.HideDatasetPath = call.Transport == TransportHTTP
	return a.Execute(ctx, invocation)
}

// BindArguments maps tool arguments onto an Invocation: command parameters by name, per-call flags
// by their underscored names. Anything else is usage, as an unknown flag is on the command line.
func BindArguments(command *registry.Command, arguments json.RawMessage, serverFlags map[string]string) (*Invocation, *errs.Error) {
	invocation := &Invocation{Command: command, Params: map[string]any{}, Flags: map[string]string{}}
	for key, value := range serverFlags {
		invocation.Flags[key] = value
	}
	values := map[string]any{}
	if trimmed := bytes.TrimSpace(arguments); len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.UseNumber()
		if err := decoder.Decode(&values); err != nil {
			return nil, errs.Usagef("Tool arguments must be a JSON object.")
		}
	}
	flags := map[string]*registry.FlagDef{}
	for _, flag := range perCallFlags(command) {
		flags[ArgumentName(flag)] = flag
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := values[key]
		if command.Param(key) != nil {
			invocation.Params[key] = value
			continue
		}
		flag, ok := flags[key]
		if !ok {
			return nil, errs.Usagef("%s has no argument %q.", command.Name, key).
				WithHint(registry.ToolName+" describe "+command.Name).WithDetail("unknown_argument", key)
		}
		text, err := flagValue(flag, key, value)
		if err != nil {
			return nil, err
		}
		if text != "" {
			invocation.Flags[flag.Name] = text
		}
	}
	return invocation, nil
}

// flagValue renders a tool argument in the string form argv parsing stores. A false boolean is
// the same as leaving the flag out.
func flagValue(flag *registry.FlagDef, key string, value any) (string, *errs.Error) {
	if value == nil {
		return "", nil
	}
	switch typed := value.(type) {
	case bool:
		if flag.Value != "" {
			return "", errs.Usagef("%s takes a value, not true or false.", key).WithDetail("argument", key)
		}
		if typed {
			return "true", nil
		}
		return "", nil
	case json.Number:
		if flag.Value == "" {
			return "", errs.Usagef("%s takes true or false.", key).WithDetail("argument", key)
		}
		return typed.String(), nil
	case string:
		if flag.Value == "" {
			return "", errs.Usagef("%s takes true or false.", key).WithDetail("argument", key)
		}
		return typed, nil
	}
	return "", errs.Usagef("%s has an unsupported type %T.", key, value).WithDetail("argument", key)
}
