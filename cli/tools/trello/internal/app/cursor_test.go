package app

import (
	"testing"

	"github.com/guyravid/ai/cli/tools/trello/internal/envelope"
	"github.com/guyravid/ai/cli/tools/trello/internal/registry"
)

// A cursor stores parameters as text; typedParam must give each back as the type the command declares.
func TestCursorRoundTripRestoresParameterTypes(t *testing.T) {
	command := &registry.Command{Name: "things.list", Params: []registry.Param{
		{Name: "board", Type: registry.TypeString},
		{Name: "after", Type: registry.TypeInteger},
		{Name: "archived", Type: registry.TypeBoolean},
	}}
	request := &listRequest{params: map[string]any{"board": "B", "after": int64(4200000000), "archived": true}, limit: 10}
	text := envelope.EncodeCursor(envelope.Cursor{C: command.Name, Q: (&session{}).cursorParams(request)})

	cursor, err := envelope.DecodeCursor(text, command.Name)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"board": "B", "after": int64(4200000000), "archived": true}
	for name, expected := range want {
		if got := typedParam(command, name, cursor.Q[name]); got != expected {
			t.Errorf("%s = %#v (%T), want %#v (%T)", name, got, got, expected, expected)
		}
	}
	if got := typedParam(command, "unknown", "7"); got != "7" {
		t.Errorf("an undeclared parameter stays text, got %#v", got)
	}
	if got := typedParam(command, "after", "not-a-number"); got != "not-a-number" {
		t.Errorf("an unparsable integer stays text, got %#v", got)
	}
}
