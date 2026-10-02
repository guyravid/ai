package envelope

import (
	"encoding/base64"
	"encoding/json"

	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
)

// Cursor is the continuation the agent passes back with --cursor (patterns/envelope.md §8).
type Cursor struct {
	V int               `json:"v"`
	C string            `json:"c"`
	U string            `json:"u,omitempty"`
	D string            `json:"d,omitempty"`
	O *int64            `json:"o,omitempty"`
	Q map[string]string `json:"q,omitempty"`
	W *Window           `json:"w,omitempty"`
}

func EncodeCursor(cursor Cursor) string {
	cursor.V = 1
	encoded, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(encoded)
}

// DecodeCursor validates a cursor and checks that it belongs to command.
func DecodeCursor(text, command string) (*Cursor, *errs.Error) {
	raw, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		return nil, malformed()
	}
	var cursor Cursor
	if err := json.Unmarshal(raw, &cursor); err != nil || cursor.C == "" {
		return nil, malformed()
	}
	if cursor.V != 1 {
		return nil, errs.Usagef("The cursor has an unsupported format version.").
			WithHint("Re-run the original query without --cursor.")
	}
	if cursor.C != command {
		return nil, errs.Usagef("This cursor was issued by %s and cannot be used with %s.", cursor.C, command).
			WithDetail("cursor_command", cursor.C)
	}
	return &cursor, nil
}

func malformed() *errs.Error {
	return errs.Usagef("The cursor is malformed.").
		WithHint("Pass meta.page.next_cursor back unchanged, or re-run the query without --cursor.")
}
