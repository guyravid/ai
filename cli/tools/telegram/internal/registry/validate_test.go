package registry

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
)

type boardInput struct {
	Board string `json:"board" jsonschema:"Board id"`
}

type idInput struct {
	ID string `json:"id" jsonschema:"Card id" cli:"positional"`
}

type clashInput struct {
	Limit string `json:"limit" jsonschema:"collides with --limit"`
}

type credentialInput struct {
	Token string `json:"token" jsonschema:"never allowed"`
}

type record struct {
	ID string `json:"id"`
}

var cardFields = &FieldSet{Default: []string{"id"}, Available: []string{"id"}}

func listSpec(name string) Spec {
	return Spec{Name: name, Description: "List things.", Fields: cardFields,
		Sort: "id asc", Limits: &Limits{Default: 25, Max: 100}, Errors: []errs.Code{errs.Usage}}
}

// object defines an object command with fields, so only the rule under test can fail.
func object[In any](spec Spec) *Command {
	if spec.Fields == nil {
		spec.Fields = cardFields
	}
	return Object[In, record](spec, nil)
}

func write(spec Spec) *Command { return Write[idInput, record](spec, nil) }

func validate(commands ...*Command) error { return New(commands).Validate() }

func TestValidRegistryPasses(t *testing.T) {
	err := validate(
		List[boardInput, record](listSpec("cards.list"), nil),
		object[idInput](Spec{Name: "cards.get", Description: "Get a card."}),
		write(Spec{Name: "cards.archive", Description: "Archive a card."}),
		write(Spec{Name: "write.cards.close", Description: "Close a card."}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := New(Builtins()).Validate(); err != nil {
		t.Fatalf("builtins: %v", err)
	}
}

func TestValidationRejects(t *testing.T) {
	longDescription := strings.Repeat("x", 161)
	missingSort := listSpec("cards.list")
	missingSort.Sort = ""
	get := func(spec Spec) Spec {
		spec.Name, spec.Description = "cards.get", "Get."
		return spec
	}

	cases := map[string]struct {
		commands []*Command
		want     string
	}{
		"duplicate":              {[]*Command{object[idInput](get(Spec{})), object[idInput](get(Spec{}))}, "defined twice"},
		"uppercase segment":      {[]*Command{object[idInput](Spec{Name: "Cards.get", Description: "Get."})}, "must match"},
		"underscore segment":     {[]*Command{object[idInput](Spec{Name: "cards.get_one", Description: "Get."})}, "must match"},
		"one segment":            {[]*Command{object[idInput](Spec{Name: "cards", Description: "Get."})}, "two segments"},
		"reserved group":         {[]*Command{object[idInput](Spec{Name: "dataset.peek", Description: "Peek."})}, "reserved"},
		"reserved after write":   {[]*Command{write(Spec{Name: "write.teach.reset", Description: "Reset."})}, "reserved"},
		"short write prefix":     {[]*Command{write(Spec{Name: "write.cards", Description: "W."})}, "group and a verb"},
		"read with write prefix": {[]*Command{object[idInput](Spec{Name: "write.cards.get", Description: "Get."})}, "only a mutating command"},
		"empty description":      {[]*Command{object[idInput](Spec{Name: "cards.get"})}, "description"},
		"long description":       {[]*Command{object[idInput](Spec{Name: "cards.get", Description: longDescription})}, "description"},
		"multiline":              {[]*Command{object[idInput](Spec{Name: "cards.get", Description: "a\nb"})}, "description"},
		"list without sort":      {[]*Command{List[boardInput, record](missingSort, nil)}, "fields, sort, and limits"},
		"collectable object":     {[]*Command{object[idInput](get(Spec{Collectable: true}))}, "collectable requires paged"},
		"windowed object":        {[]*Command{object[idInput](get(Spec{TimeWindow: true}))}, "time window"},
		"reserved flag":          {[]*Command{object[clashInput](get(Spec{}))}, "reserved flag"},
		"credential flag":        {[]*Command{object[credentialInput](get(Spec{}))}, "forbidden credential"},
		"unknown error code":     {[]*Command{object[idInput](get(Spec{Errors: []errs.Code{"teapot"}}))}, "not in the contract"},
		"object without fields":  {[]*Command{Object[idInput, record](get(Spec{}), nil)}, "declares fields"},
	}
	for name, tc := range cases {
		err := validate(tc.commands...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", name, err, tc.want)
		}
	}
}

func TestReadOnlyHintFollowsKind(t *testing.T) {
	command := object[idInput](Spec{Name: "cards.get", Description: "Get."})
	command.Annotations.ReadOnlyHint = false
	if err := validate(command); err == nil || !strings.Contains(err.Error(), "readOnlyHint") {
		t.Errorf("got %v", err)
	}
	mutating := write(Spec{Name: "cards.archive", Description: "Archive.", Destructive: true})
	if mutating.Annotations.ReadOnlyHint || mutating.Annotations.DestructiveHint == nil || !*mutating.Annotations.DestructiveHint {
		t.Errorf("write annotations %+v", mutating.Annotations)
	}
}

func TestValidateMutating(t *testing.T) {
	registry := New([]*Command{
		write(Spec{Name: "cards.archive", Description: "Archive."}),
		write(Spec{Name: "cards.close", Description: "Close."}),
		object[idInput](Spec{Name: "cards.get", Description: "Get."}),
	})
	if err := registry.ValidateMutating([]string{"cards.archive", "cards.close"}); err != nil {
		t.Errorf("matching list: %v", err)
	}
	if err := registry.ValidateMutating([]string{"cards.archive"}); err == nil {
		t.Error("a missing name must fail")
	}
	if err := registry.ValidateMutating([]string{"cards.archive", "cards.close", "cards.delete"}); err == nil {
		t.Error("an extra name must fail")
	}
	// A read-only registry has no writes to compare, so any list is accepted.
	if err := New(Builtins()).ValidateMutating([]string{"cards.archive"}); err != nil {
		t.Errorf("read-only registry: %v", err)
	}
}

func TestParamsDerivedFromInput(t *testing.T) {
	type input struct {
		ID     string `json:"id" jsonschema:"Card" cli:"positional"`
		ListID string `json:"list_id,omitempty" jsonschema:"List"`
		Count  int    `json:"count,omitempty"`
		Closed bool   `json:"closed,omitempty"`
	}
	command := object[input](Spec{Name: "cards.get", Description: "Get.", Enums: map[string][]string{"list_id": {"a", "b"}}})
	params := command.Params
	if len(params) != 4 || !params[0].Positional || !params[0].Required || params[0].Flag != "" {
		t.Fatalf("params %+v", params)
	}
	if params[1].Flag != "--list-id" || params[1].Required || len(params[1].Enum) != 2 {
		t.Errorf("list_id %+v", params[1])
	}
	if params[2].Type != TypeInteger || params[3].Type != TypeBoolean {
		t.Errorf("types %s %s", params[2].Type, params[3].Type)
	}
	if enum := command.InputSchema.Properties["list_id"].Enum; len(enum) != 2 {
		t.Errorf("schema enum %v", enum)
	}
	if _, err := command.Decode(map[string]any{"id": "1", "list_id": "c"}); err == nil || err.Code != errs.Usage {
		t.Errorf("enum violation: %v", err)
	}
	if _, err := command.Decode(map[string]any{"list_id": "a"}); err == nil || err.Details["missing"] != "id" {
		t.Errorf("missing positional: %v", err)
	}
	if _, err := command.Decode(map[string]any{"id": "1", "bogus": "x"}); err == nil {
		t.Error("unknown parameter must fail")
	}
	if _, err := command.Decode(map[string]any{"id": "1", "count": "many"}); err == nil {
		t.Error("wrong type must fail")
	}
}

// The output schema admits every shaped response (contract §6.2).
func TestOutputSchemaIsOpen(t *testing.T) {
	type label struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type card struct {
		ID     string      `json:"id"`
		Name   string      `json:"name"`
		Pos    json.Number `json:"pos"`
		Labels []label     `json:"labels"`
		Badges struct {
			Votes int `json:"votes"`
		} `json:"badges"`
	}
	list := List[boardInput, card](listSpec("cards.list"), nil)
	encoded, _ := json.Marshal(list.OutputSchema)
	text := string(encoded)
	for _, forbidden := range []string{`"required"`, `"additionalProperties":false`} {
		if strings.Contains(text, forbidden) {
			t.Errorf("schema contains %s: %s", forbidden, text)
		}
	}
	item := list.OutputSchema.Items
	if item.AnyOf != nil || item.Type != "object" {
		t.Errorf("the record itself is never elided: %s", text)
	}
	for _, name := range []string{"labels", "badges"} {
		property := item.Properties[name]
		if len(property.AnyOf) != 2 || property.AnyOf[1].Const == nil || *property.AnyOf[1].Const != "<depth-elided>" {
			t.Errorf("%s must admit <depth-elided>: %+v", name, property)
		}
	}
	if labelItems := item.Properties["labels"].AnyOf[0].Items; len(labelItems.AnyOf) != 2 {
		t.Errorf("objects inside arrays must admit <depth-elided>: %+v", labelItems)
	}
	if item.Properties["id"].AnyOf != nil || item.Properties["pos"].Type != "number" {
		t.Errorf("scalars keep their type: id %+v pos %+v", item.Properties["id"], item.Properties["pos"])
	}

	single := Object[idInput, card](Spec{Name: "cards.get", Description: "Get.", Fields: cardFields}, nil)
	if single.OutputSchema.AnyOf != nil || len(single.OutputSchema.Required) != 0 {
		t.Errorf("object record: %+v", single.OutputSchema)
	}
}

type anyChatInput struct {
	Chat         string `json:"chat,omitempty" jsonschema:"Chat"`
	AllowAnyChat bool   `json:"allow_any_chat,omitempty" jsonschema:"Any chat"`
}

func TestUnavailableParamIsAbsentFromEverySurfaceButStillRecognised(t *testing.T) {
	refusal := func() *errs.Error {
		return errs.New(errs.Refused, "not in this build").WithDetail("reason", "not_built")
	}
	command := object[anyChatInput](Spec{Name: "chats.get", Description: "Get.", Unavailable: []Unavailable{{Name: "allow_any_chat", Err: refusal}}})
	if command.Param("allow_any_chat") != nil || command.Param("chat") == nil {
		t.Errorf("params %+v", command.Params)
	}
	if _, present := command.InputSchema.Properties["allow_any_chat"]; present {
		t.Error("the input schema must not list it")
	}
	for _, name := range command.InputSchema.PropertyOrder {
		if name == "allow_any_chat" {
			t.Error("property order must not list it")
		}
	}
	for _, spelling := range []string{"allow_any_chat", "allow-any-chat"} {
		if err := command.UnavailableParam(spelling); err == nil || err.Code != errs.Refused || err.Details["reason"] != "not_built" {
			t.Errorf("%s: got %v", spelling, err)
		}
	}
	if command.UnavailableParam("bogus") != nil {
		t.Error("an unknown name is not an unavailable one")
	}
	if _, err := command.Decode(map[string]any{"allow_any_chat": true}); err == nil {
		t.Error("decoding it must fail: the parameter does not exist in this build")
	}
}
