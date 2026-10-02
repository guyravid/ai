// Package registry holds every command the tool exposes. Discovery, teach, argv parsing, help, and
// the MCP server are all generated from these entries (base template §2).
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
	"github.com/guyravid/ai/cli/tools/trello/internal/shape"
	"github.com/guyravid/ai/cli/tools/trello/internal/upstream"
)

// ToolName is the binary name used in hints.
const ToolName = "trello"

type Kind string

const (
	KindObject  Kind = "object"  // returns one resource
	KindList    Kind = "list"    // returns a list, paged by the substrate
	KindWrite   Kind = "write"   // builds one mutating request
	KindBuiltin Kind = "builtin" // contract-provided; run by the application layer
)

type Annotations struct {
	ReadOnlyHint    bool  `json:"readOnlyHint"`
	DestructiveHint *bool `json:"destructiveHint,omitempty"`
	IdempotentHint  bool  `json:"idempotentHint"`
	OpenWorldHint   bool  `json:"openWorldHint"`
}

type ParamType string

const (
	TypeString  ParamType = "string"
	TypeBoolean ParamType = "boolean"
	TypeInteger ParamType = "integer"
)

// Param is one input, derived from a field of the command's input struct.
type Param struct {
	Name        string
	Flag        string // "--board"; empty for positional parameters
	Positional  bool
	Required    bool
	Type        ParamType
	Description string
	Enum        []string
}

type FieldSet struct {
	Default   []string `json:"default"`
	Available []string `json:"available"`
}

type Limits struct {
	Default int `json:"default_limit"`
	Max     int `json:"max_limit"`
}

type Example struct {
	Argv        []string `json:"argv"`
	Description string   `json:"description"`
}

// Window is the resolved time range for a command with a time dimension.
type Window struct {
	Since time.Time
	Until time.Time
}

// Doer sends upstream requests. Handlers receive it rather than building their own client.
type Doer interface {
	Do(ctx context.Context, request *upstream.Request) (shape.Value, error)
}

// Call is everything a handler may use. Handlers read no environment, print nothing, and never
// touch datasets (base template §3).
type Call struct {
	Upstream Doer
	Window   *Window
	Sort     []shape.SortKey
}

// Batch is one upstream page, already sorted.
type Batch struct {
	Records []shape.Value
	Next    string // continuation for the following page; "" when exhausted
	Total   *int64 // total matching records, when upstream reports it
}

// ListSource is the pagination adapter for one list command: the only place the API's own paging
// style is visible (base template §9.2).
type ListSource interface {
	Fetch(ctx context.Context, continuation string, want int) (Batch, error)
	// MaxPageSize is the largest page upstream allows; --all always uses it.
	MaxPageSize() int
	// Resume returns the continuation that picks up right after the delivered records, given the
	// continuation this invocation started from.
	Resume(start string, delivered []shape.Value) string
}

type Command struct {
	Name        string
	Description string
	Kind        Kind
	Annotations Annotations
	Params      []Param
	Paged       bool
	Collectable bool
	TimeWindow  bool
	Fields      *FieldSet
	Sort        string
	Limits      *Limits
	Errors      []errs.Code
	Examples    []Example

	InputSchema  *jsonschema.Schema
	OutputSchema *jsonschema.Schema

	decode     func(params map[string]any) (any, error)
	runObject  func(ctx context.Context, call *Call, input any) (shape.Value, error)
	runList    func(ctx context.Context, call *Call, input any) (ListSource, error)
	buildWrite func(ctx context.Context, call *Call, input any) (*upstream.Request, error)
}

// Argv is the command line path: the name split on dots.
func (c *Command) Argv() []string { return strings.Split(c.Name, ".") }

// IsWrite reports whether the command changes upstream state. It is declared by defining the
// command with Write, never inferred from the name (contract 1.1, §11.1).
func (c *Command) IsWrite() bool { return c.Kind == KindWrite }

// HasWritePrefix reports whether the name uses the optional write prefix.
func (c *Command) HasWritePrefix() bool { return strings.HasPrefix(c.Name, "write.") }

func (c *Command) Param(name string) *Param {
	for index := range c.Params {
		if c.Params[index].Name == name {
			return &c.Params[index]
		}
	}
	return nil
}

func (c *Command) Positional() []string {
	var names []string
	for _, param := range c.Params {
		if param.Positional {
			names = append(names, param.Name)
		}
	}
	return names
}

// Decode validates parameters against the command's input type.
func (c *Command) Decode(params map[string]any) (any, *errs.Error) {
	for _, param := range c.Params {
		value, present := params[param.Name]
		if !present || value == "" {
			if param.Required {
				how := param.Flag + " <" + param.Name + ">"
				if param.Positional {
					how = "<" + param.Name + ">"
				}
				return nil, errs.Usagef("%s requires %s.", strings.Join(c.Argv(), " "), how).
					WithHint(fmt.Sprintf("%s describe %s", ToolName, c.Name)).
					WithDetail("missing", param.Name)
			}
			continue
		}
		if len(param.Enum) > 0 {
			text, _ := value.(string)
			if !contains(param.Enum, text) {
				return nil, errs.Usagef("%s must be one of %s.", displayName(param), strings.Join(param.Enum, ", ")).
					WithDetail("parameter", param.Name).WithDetail("allowed", param.Enum)
			}
		}
	}
	for name := range params {
		if c.Param(name) == nil {
			return nil, errs.Usagef("%s has no parameter %q.", c.Name, name).WithHint(ToolName + " describe " + c.Name)
		}
	}
	if c.decode == nil {
		return nil, nil
	}
	input, err := c.decode(params)
	if err != nil {
		return nil, errs.Usagef("Invalid parameters for %s: %s.", c.Name, err.Error()).WithHint(ToolName + " describe " + c.Name)
	}
	return input, nil
}

func (c *Command) RunObject(ctx context.Context, call *Call, input any) (shape.Value, error) {
	return c.runObject(ctx, call, input)
}

func (c *Command) RunList(ctx context.Context, call *Call, input any) (ListSource, error) {
	return c.runList(ctx, call, input)
}

func (c *Command) BuildWrite(ctx context.Context, call *Call, input any) (*upstream.Request, error) {
	return c.buildWrite(ctx, call, input)
}

func displayName(param Param) string {
	if param.Positional {
		return "<" + param.Name + ">"
	}
	return param.Flag
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// Spec is what a command author writes; the generic constructors fill in the rest from types.
type Spec struct {
	Name        string
	Description string
	Destructive bool
	Idempotent  bool
	TimeWindow  bool
	Collectable bool
	Fields      *FieldSet
	Sort        string
	Limits      *Limits
	Enums       map[string][]string
	Errors      []errs.Code
	Examples    []Example
}

// Object defines a command that returns one resource.
func Object[In, Out any](spec Spec, run func(context.Context, *Call, In) (shape.Value, error)) *Command {
	command := buildCommand[In, Out](spec, KindObject, false)
	command.runObject = func(ctx context.Context, call *Call, input any) (shape.Value, error) {
		return run(ctx, call, input.(In))
	}
	return command
}

// List defines a command that returns a list. Out is the type of one record.
func List[In, Out any](spec Spec, run func(context.Context, *Call, In) (ListSource, error)) *Command {
	command := buildCommand[In, []Out](spec, KindList, true)
	command.runList = func(ctx context.Context, call *Call, input any) (ListSource, error) {
		return run(ctx, call, input.(In))
	}
	return command
}

// Write defines a mutating command. Its handler only builds the request; the substrate decides
// whether to preview, dry-run, or send it.
func Write[In, Out any](spec Spec, build func(context.Context, *Call, In) (*upstream.Request, error)) *Command {
	command := buildCommand[In, Out](spec, KindWrite, false)
	command.buildWrite = func(ctx context.Context, call *Call, input any) (*upstream.Request, error) {
		return build(ctx, call, input.(In))
	}
	return command
}

// Builtin defines a contract-provided command. The application layer runs it by name.
func Builtin[In any](spec Spec, paged bool) *Command {
	return buildCommand[In, any](spec, KindBuiltin, paged)
}

func buildCommand[In, Out any](spec Spec, kind Kind, paged bool) *Command {
	readOnly := kind != KindWrite
	command := &Command{
		Name:        spec.Name,
		Description: spec.Description,
		Kind:        kind,
		Annotations: Annotations{
			ReadOnlyHint:   readOnly,
			IdempotentHint: readOnly || spec.Idempotent,
			OpenWorldHint:  kind != KindBuiltin,
		},
		Paged:       paged,
		Collectable: spec.Collectable,
		TimeWindow:  spec.TimeWindow,
		Fields:      spec.Fields,
		Sort:        spec.Sort,
		Limits:      spec.Limits,
		Errors:      spec.Errors,
		Examples:    spec.Examples,
	}
	if !readOnly {
		destructive := spec.Destructive
		command.Annotations.DestructiveHint = &destructive
	}
	command.Params = paramsOf(reflect.TypeFor[In](), spec.Enums)
	command.InputSchema = mustSchema(reflect.TypeFor[In]())
	for _, param := range command.Params {
		if property := command.InputSchema.Properties[param.Name]; property != nil && len(param.Enum) > 0 {
			property.Enum = make([]any, len(param.Enum))
			for index, value := range param.Enum {
				property.Enum[index] = value
			}
		}
	}
	command.OutputSchema = openOutputSchema(mustSchema(reflect.TypeFor[Out]()), kind == KindList)
	command.decode = func(params map[string]any) (any, error) {
		encoded, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		var input In
		decoder := json.NewDecoder(strings.NewReader(string(encoded)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			return nil, err
		}
		return input, nil
	}
	return command
}

var rawSchema = &jsonschema.Schema{}

// numberSchema describes json.Number, which is a string in Go but a number on the wire.
var numberSchema = &jsonschema.Schema{Type: "number"}

// depthElided is what --max-depth leaves in place of a subtree (contract §8.4).
var depthElided any = "<depth-elided>"

// openOutputSchema makes a derived output schema admit every shaped response (contract §6.2).
// Derivation requires each field without omitempty and closes every object, but --fields,
// empty-stripping, and the size caps remove and replace values per call. For a list the record is
// each item; otherwise the record is the schema itself. A record is never depth-elided.
func openOutputSchema(schema *jsonschema.Schema, list bool) *jsonschema.Schema {
	if list && schema.Items != nil {
		record := openSchema(schema.Items, false)
		schema.Items = nil
		schema = openSchema(schema, false)
		schema.Items = record
		return schema
	}
	return openSchema(schema, false)
}

func openSchema(schema *jsonschema.Schema, nested bool) *jsonschema.Schema {
	if schema == nil || schema == rawSchema {
		return schema
	}
	schema.Required = nil
	if isFalseSchema(schema.AdditionalProperties) {
		schema.AdditionalProperties = nil
	}
	if hasType(schema, "string") {
		schema.MaxLength, schema.Pattern, schema.Enum, schema.Const = nil, "", nil, nil
	}
	for name, property := range schema.Properties {
		schema.Properties[name] = openSchema(property, true)
	}
	if schema.Items != nil {
		schema.Items = openSchema(schema.Items, true)
	}
	for index, item := range schema.PrefixItems {
		schema.PrefixItems[index] = openSchema(item, true)
	}
	if schema.AdditionalProperties != nil {
		schema.AdditionalProperties = openSchema(schema.AdditionalProperties, true)
	}
	for _, branches := range [][]*jsonschema.Schema{schema.AnyOf, schema.OneOf, schema.AllOf} {
		for index, branch := range branches {
			branches[index] = openSchema(branch, nested)
		}
	}
	if nested && (hasType(schema, "object") || hasType(schema, "array")) {
		return &jsonschema.Schema{AnyOf: []*jsonschema.Schema{schema, {Const: &depthElided}}}
	}
	return schema
}

func hasType(schema *jsonschema.Schema, name string) bool {
	return schema.Type == name || slices.Contains(schema.Types, name)
}

// isFalseSchema reports the schema that matches nothing, as jsonschema-go writes it: {"not":{}}.
func isFalseSchema(schema *jsonschema.Schema) bool {
	if schema == nil || schema.Not == nil {
		return false
	}
	encoded, err := json.Marshal(schema)
	return err == nil && string(encoded) == "false"
}

func mustSchema(t reflect.Type) *jsonschema.Schema {
	if t.Kind() == reflect.Interface {
		return &jsonschema.Schema{}
	}
	schema, err := jsonschema.ForType(t, &jsonschema.ForOptions{
		TypeSchemas: map[reflect.Type]*jsonschema.Schema{
			reflect.TypeFor[json.RawMessage](): rawSchema,
			reflect.TypeFor[shape.Value]():     rawSchema,
			reflect.TypeFor[json.Number]():     numberSchema,
		},
	})
	if err != nil {
		panic(fmt.Sprintf("schema for %s: %v", t, err))
	}
	return schema
}

// paramsOf derives parameters from the input struct: the json tag names the parameter, jsonschema
// describes it, and `cli:"positional"` makes it positional. Everything else becomes --kebab-name.
func paramsOf(t reflect.Type, enums map[string][]string) []Param {
	if t.Kind() != reflect.Struct {
		return nil
	}
	var params []Param
	for index := 0; index < t.NumField(); index++ {
		field := t.Field(index)
		tag := field.Tag.Get("json")
		name, options, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		param := Param{
			Name:        name,
			Required:    !strings.Contains(options, "omitempty"),
			Description: field.Tag.Get("jsonschema"),
			Enum:        enums[name],
		}
		switch field.Type.Kind() {
		case reflect.Bool:
			param.Type = TypeBoolean
		case reflect.Int, reflect.Int64:
			param.Type = TypeInteger
		default:
			param.Type = TypeString
		}
		if field.Tag.Get("cli") == "positional" {
			param.Positional = true
		} else {
			param.Flag = "--" + strings.ReplaceAll(name, "_", "-")
		}
		params = append(params, param)
	}
	return params
}

// Registry is the ordered set of commands in this build.
type Registry struct {
	commands []*Command
	byName   map[string]*Command
}

func New(commands []*Command) *Registry {
	sorted := append([]*Command(nil), commands...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	registry := &Registry{commands: sorted, byName: map[string]*Command{}}
	for _, command := range sorted {
		registry.byName[command.Name] = command
	}
	return registry
}

func (r *Registry) Commands() []*Command     { return r.commands }
func (r *Registry) Get(name string) *Command { return r.byName[name] }

func (r *Registry) Names() []string {
	names := make([]string, len(r.commands))
	for index, command := range r.commands {
		names[index] = command.Name
	}
	return names
}
