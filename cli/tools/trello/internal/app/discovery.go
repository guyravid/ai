package app

import (
	"encoding/json"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/guyravid/ai/cli/tools/trello/internal/envelope"
	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
	"github.com/guyravid/ai/cli/tools/trello/internal/registry"
	"github.com/guyravid/ai/cli/tools/trello/internal/shape"
)

type toolDetail struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Mutates     bool   `json:"mutates"`
}

func (a *App) tools(invocation *Invocation) shape.Value {
	if invocation.Bool("detail") {
		details := make([]toolDetail, 0, len(a.Registry.Commands()))
		for _, command := range a.Registry.Commands() {
			details = append(details, toolDetail{Name: command.Name, Description: command.Description, Mutates: command.IsWrite()})
		}
		return mustValue(details)
	}
	return mustValue(a.Registry.Names())
}

type describeDocument struct {
	Tool            string          `json:"tool"`
	ToolVersion     string          `json:"tool_version"`
	ContractVersion string          `json:"contract_version"`
	WritesEnabled   bool            `json:"writes_enabled"`
	MCPEnabled      bool            `json:"mcp_enabled"`
	Auth            describeAuth    `json:"auth"`
	ExitCodes       []exitCode      `json:"exit_codes"`
	EnvelopeSchema  json.RawMessage `json:"envelope_schema"`
	Commands        []commandEntry  `json:"commands"`
}

type describeAuth struct {
	Credentials []credentialRef `json:"credentials"`
}

type credentialRef struct {
	Name string `json:"name"`
	Env  string `json:"env"`
}

type exitCode struct {
	Code      int    `json:"code"`
	Name      string `json:"name"`
	Retriable bool   `json:"retriable"`
	Meaning   string `json:"meaning"`
}

// commandEntry is an MCP tool definition plus the contract's members (§6.2).
type commandEntry struct {
	Name         string               `json:"name"`
	Argv         []string             `json:"argv"`
	Description  string               `json:"description"`
	Annotations  registry.Annotations `json:"annotations"`
	InputSchema  *jsonschema.Schema   `json:"inputSchema"`
	OutputSchema *jsonschema.Schema   `json:"outputSchema"`
	XCLI         xCLI                 `json:"x-cli"`
	Fields       *registry.FieldSet   `json:"fields,omitempty"`
	Sort         string               `json:"sort,omitempty"`
	Limits       *registry.Limits     `json:"limits,omitempty"`
	Examples     []registry.Example   `json:"examples,omitempty"`
}

type xCLI struct {
	Flags       map[string]string `json:"flags"`
	Positional  []string          `json:"positional"`
	Paged       bool              `json:"paged"`
	Collectable bool              `json:"collectable"`
	TimeWindow  bool              `json:"time_window"`
	Errors      []errs.Code       `json:"errors"`
}

// Entry returns the describe entry for one command; the MCP server publishes the same data.
func Entry(command *registry.Command) commandEntry {
	flags := map[string]string{}
	positional := []string{}
	for _, param := range command.Params {
		if param.Positional {
			positional = append(positional, param.Name)
		} else {
			flags[param.Name] = param.Flag
		}
	}
	errorCodes := command.Errors
	if errorCodes == nil {
		errorCodes = []errs.Code{}
	}
	return commandEntry{
		Name: command.Name, Argv: command.Argv(), Description: command.Description,
		Annotations: command.Annotations, InputSchema: command.InputSchema, OutputSchema: command.OutputSchema,
		XCLI: xCLI{Flags: flags, Positional: positional, Paged: command.Paged, Collectable: command.Collectable,
			TimeWindow: command.TimeWindow, Errors: errorCodes},
		Fields: command.Fields, Sort: command.Sort, Limits: command.Limits, Examples: command.Examples,
	}
}

func (a *App) describe(invocation *Invocation) (shape.Value, *errs.Error) {
	commands := a.Registry.Commands()
	if name, _ := invocation.Params["name"].(string); name != "" {
		name = strings.ReplaceAll(strings.TrimSpace(name), " ", ".")
		command := a.Registry.Get(name)
		if command == nil {
			err := errs.Usagef("No command named %s.", name).WithHint(a.Build.Tool+" tools").WithDetail("unknown_command", name)
			if suggestion := shape.Closest(name, a.Registry.Names()); suggestion != "" {
				err.WithDetail("did_you_mean", suggestion)
			}
			return shape.Value{}, err
		}
		commands = []*registry.Command{command}
	}
	entries := make([]commandEntry, len(commands))
	for index, command := range commands {
		entries[index] = Entry(command)
	}
	return mustValue(describeDocument{
		Tool: a.Build.Tool, ToolVersion: a.Build.Version, ContractVersion: envelope.ContractVersion,
		WritesEnabled: a.Build.WritesEnabled, MCPEnabled: a.Build.MCPEnabled,
		Auth:           describeAuth{Credentials: a.credentialRefs()},
		ExitCodes:      []exitCode{},
		EnvelopeSchema: json.RawMessage(EnvelopeSchema),
		Commands:       entries,
	}), nil
}

func (a *App) credentialRefs() []credentialRef {
	refs := make([]credentialRef, len(Credentials))
	for index, name := range Credentials {
		refs[index] = credentialRef{Name: name, Env: a.Build.Prefix + "_" + name}
	}
	return refs
}

type versionData struct {
	Tool            string `json:"tool"`
	ToolVersion     string `json:"tool_version"`
	ContractVersion string `json:"contract_version"`
	WritesEnabled   bool   `json:"writes_enabled"`
	MCPEnabled      bool   `json:"mcp_enabled"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	Commit          string `json:"commit"`
}

func (a *App) version() shape.Value {
	return mustValue(versionData{
		Tool: a.Build.Tool, ToolVersion: a.Build.Version, ContractVersion: envelope.ContractVersion,
		WritesEnabled: a.Build.WritesEnabled, MCPEnabled: a.Build.MCPEnabled,
		OS: a.Build.GOOS, Arch: a.Build.GOARCH, Commit: a.Build.Commit,
	})
}

func mustValue(value any) shape.Value {
	converted, err := shape.FromAny(value)
	if err != nil {
		panic(err)
	}
	return converted
}

// EnvelopeSchema is the JSON Schema of contract §3, from patterns/envelope.md §1 and §2, with
// data unconstrained.
const EnvelopeSchema = `{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"agentcli/envelope.json","type":"object","required":["ok","tool","command","data","meta"],"additionalProperties":false,"properties":{"ok":{"type":"boolean"},"tool":{"type":"string"},"command":{"type":["string","null"],"pattern":"^[a-z][a-z0-9-]*(\\.[a-z][a-z0-9-]*)*$"},"data":true,"error":{"type":"object","required":["code","exit_code","message","retriable"],"additionalProperties":false,"properties":{"code":{"enum":["internal","usage","config","auth","not_found","validation","conflict","refused","rate_limited","timeout","network","upstream","partial","cache_miss","canceled"]},"exit_code":{"type":"integer"},"message":{"type":"string","minLength":1,"maxLength":512},"retriable":{"type":"boolean"},"retry_after_ms":{"type":"integer","minimum":0},"hint":{"type":"string"},"details":{"type":"object"}}},"meta":{"type":"object","required":["contract_version","tool_version"],"properties":{"contract_version":{"type":"string","pattern":"^\\d+\\.\\d+$"},"tool_version":{"type":"string"},"page":{"$ref":"#/$defs/page"},"window":{"type":"object","required":["since","until","source"],"properties":{"since":{"type":"string","format":"date-time"},"until":{"type":"string","format":"date-time"},"source":{"enum":["flag","default","cursor"]}}},"fields":{"type":"array","items":{"type":"string"}},"stripped_empty":{"const":true},"elided_fields":{"type":"array","items":{"type":"string"}},"sort":{"type":"string"},"dataset":{"type":"object"},"errors":{"type":"array","items":{"type":"object"}},"dry_run":{"const":true},"warnings":{"type":"array","minItems":1,"items":{"type":"object","required":["code","message"],"properties":{"code":{"type":"string"},"message":{"type":"string"}}}},"timing":{"type":"object"}}}},"allOf":[{"if":{"properties":{"ok":{"const":true}}},"then":{"not":{"required":["error"]}}},{"if":{"properties":{"ok":{"const":false}}},"then":{"required":["error"]}}],"$defs":{"page":{"type":"object","required":["limit","count","total","total_is_exact","has_more","next_cursor","truncated"],"additionalProperties":false,"properties":{"limit":{"type":"integer","minimum":1},"count":{"type":"integer","minimum":0},"total":{"type":["integer","null"],"minimum":0},"total_is_exact":{"type":"boolean"},"has_more":{"type":"boolean"},"next_cursor":{"type":["string","null"]},"truncated":{"type":"boolean"},"truncated_reason":{"enum":["max_pages","max_bytes","max_bytes_below_minimum","budget"]},"dropped":{"type":"integer","minimum":1}}}}}`
