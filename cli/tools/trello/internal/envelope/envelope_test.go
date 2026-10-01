package envelope

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
	"github.com/guyravid/ai/cli/tools/trello/internal/shape"
)

func command(name string) *string { return &name }

func TestSuccessShape(t *testing.T) {
	envelope := New("trello", "0.1.0", command("boards.list"))
	envelope.Data = shape.NewArray()
	got := string(Encode(envelope, false))
	want := `{"ok":true,"tool":"trello","command":"boards.list","data":[],"meta":{"contract_version":"1.1","tool_version":"0.1.0"}}` + "\n"
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestErrorShapeAndExitAgreement(t *testing.T) {
	for _, code := range errs.Codes() {
		envelope := New("trello", "0.1.0", nil)
		envelope.SetError(errs.New(code, "Something happened."))
		var decoded map[string]any
		if err := json.Unmarshal(Encode(envelope, false), &decoded); err != nil {
			t.Fatal(err)
		}
		errorBody := decoded["error"].(map[string]any)
		if int(errorBody["exit_code"].(float64)) != envelope.ExitCode() || decoded["data"] != nil || decoded["command"] != nil {
			t.Errorf("%s: %v", code, decoded)
		}
		if _, ok := decoded["ok"]; !ok {
			t.Errorf("%s: missing ok", code)
		}
	}
}

func TestDetailsAreLimited(t *testing.T) {
	fields := make([]string, 400)
	for index := range fields {
		fields[index] = fmt.Sprintf("field_number_%d", index)
	}
	envelope := New("trello", "0.1.0", nil)
	envelope.SetError(errs.Usagef("Bad field.").WithDetail("available_fields", fields).WithDetail("field", "x"))
	encoded, _ := json.Marshal(envelope.Error.Details)
	if len(encoded) > maxDetailsBytes {
		t.Fatalf("details are %d bytes", len(encoded))
	}
	if envelope.Error.Details["field"] != "x" {
		t.Error("small member was dropped instead of the large one")
	}
}

func TestCursorRoundTrip(t *testing.T) {
	offset := int64(25)
	text := EncodeCursor(Cursor{C: "cards.list", U: "25", O: &offset, Q: map[string]string{"board": "5f2a"}})
	cursor, err := DecodeCursor(text, "cards.list")
	if err != nil || cursor.Q["board"] != "5f2a" || *cursor.O != 25 {
		t.Fatalf("got %+v, %v", cursor, err)
	}
	if _, err := DecodeCursor(text, "boards.list"); err == nil || err.Code != errs.Usage {
		t.Errorf("cross-command cursor accepted: %v", err)
	}
	if _, err := DecodeCursor("!!not-a-cursor", "cards.list"); err == nil || err.Code != errs.Usage {
		t.Errorf("malformed cursor accepted: %v", err)
	}
}

func records(count int) []shape.Value {
	out := make([]shape.Value, count)
	for index := range out {
		out[index] = shape.NewObject(
			shape.Field{Key: "id", Value: shape.String(fmt.Sprintf("id-%03d", index))},
			shape.Field{Key: "desc", Value: shape.String(strings.Repeat("x", 200))},
		)
	}
	return out
}

func listEnvelope(limit int) *Envelope {
	envelope := New("trello", "0.1.0", command("cards.list"))
	envelope.Meta.Page = &Page{Limit: limit, Count: limit, Truncated: false}
	return envelope
}

func TestFitListDropsWholeRecordsAndResumes(t *testing.T) {
	envelope := listEnvelope(20)
	var resumeAt = -1
	encoded := FitList(envelope, records(20), 2000, false, func(index int) string {
		resumeAt = index
		return fmt.Sprintf("cursor-%d", index)
	})
	if len(encoded) > 2000 {
		t.Fatalf("output is %d bytes", len(encoded))
	}
	var decoded struct {
		Data []map[string]any
		Meta Meta
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	page := decoded.Meta.Page
	if !page.Truncated || page.TruncatedReason != "max_bytes" || page.Count != len(decoded.Data) ||
		page.Dropped != 20-page.Count || !page.HasMore || *page.NextCursor != fmt.Sprintf("cursor-%d", page.Count) {
		t.Fatalf("page = %+v", page)
	}
	if resumeAt != page.Count {
		t.Errorf("cursor built for %d, kept %d", resumeAt, page.Count)
	}
}

func TestFitListShrinksOneOversizedRecord(t *testing.T) {
	envelope := listEnvelope(1)
	big := []shape.Value{shape.NewObject(shape.Field{Key: "desc", Value: shape.String(strings.Repeat("y", 5000))})}
	encoded := FitList(envelope, big, 600, false, func(int) string { return "c" })
	if len(encoded) > 600 {
		t.Fatalf("output is %d bytes", len(encoded))
	}
	if len(envelope.Meta.ElidedFields) != 1 || envelope.Meta.ElidedFields[0] != "[0].desc" {
		t.Errorf("elided = %v", envelope.Meta.ElidedFields)
	}
}

func TestFitObjectKeepsData(t *testing.T) {
	envelope := New("trello", "0.1.0", command("cards.get"))
	object := shape.NewObject(
		shape.Field{Key: "id", Value: shape.String("1")},
		shape.Field{Key: "desc", Value: shape.String(strings.Repeat("z", 10000))},
	)
	encoded := FitObject(envelope, object, 1024, false)
	if len(encoded) > 1024 || envelope.Data.IsNull() {
		t.Fatalf("size %d, data null %v", len(encoded), envelope.Data.IsNull())
	}
}
