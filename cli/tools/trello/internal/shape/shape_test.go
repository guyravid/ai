package shape

import (
	"strings"
	"testing"

	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
)

func mustParse(t *testing.T, text string) Value {
	t.Helper()
	value, err := Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestParseKeepsOrderAndNumbers(t *testing.T) {
	input := `{"z":1,"a":12345678901234567890,"m":1.50,"s":"x<y&z","n":null,"b":false,"l":[3,{"k":"v"}]}`
	if got := string(mustParse(t, input).Marshal()); got != input {
		t.Fatalf("round trip changed output:\n got %s\nwant %s", got, input)
	}
}

var available = []string{"id", "name", "desc", "due", "badges.*", "labels"}
var defaults = []string{"id", "name", "due"}

func TestResolveFields(t *testing.T) {
	cases := []struct {
		expression string
		want       string
	}{
		{"", "id,name,due"},
		{"name,id", "name,id"},
		{"-due", "id,name"},
		{"badges.comments", "badges.comments"},
		{"badges.*", "badges.*"},
		{"*", "id,name,desc,due,badges.*,labels"},
	}
	for _, tc := range cases {
		paths, err := ResolveFields(tc.expression, defaults, available)
		if err != nil {
			t.Fatalf("%q: %v", tc.expression, err)
		}
		if got := strings.Join(paths, ","); got != tc.want {
			t.Errorf("%q = %s, want %s", tc.expression, got, tc.want)
		}
	}
}

func TestResolveFieldsErrors(t *testing.T) {
	for _, expression := range []string{"id,-name", "nmae", "labels[0]", "badges.*.x", "$.id"} {
		_, err := ResolveFields(expression, defaults, available)
		if err == nil || err.Code != errs.Usage {
			t.Errorf("%q: got %v, want usage", expression, err)
		}
	}
	_, err := ResolveFields("nmae", defaults, available)
	if err.Details["did_you_mean"] != "name" {
		t.Errorf("did_you_mean = %v", err.Details["did_you_mean"])
	}
}

func TestProjectNestedInRequestedOrder(t *testing.T) {
	record := mustParse(t, `{"id":"1","name":"n","badges":{"comments":2,"votes":0},"extra":true}`)
	got := string(Project(record, []string{"badges.votes", "name", "badges.comments", "missing"}).Marshal())
	want := `{"badges":{"votes":0,"comments":2},"name":"n"}`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestStripEmpty(t *testing.T) {
	record := mustParse(t, `{"a":null,"b":"","c":[],"d":{},"e":false,"f":0,"g":{"h":null},"i":[null,{"j":""}]}`)
	got := string(StripEmpty(record).Marshal())
	want := `{"e":false,"f":0,"i":[null,{}]}`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestCapStringAndDepth(t *testing.T) {
	record := mustParse(t, `{"desc":"ééééééééé","deep":{"a":{"b":{"c":1}}}}`)
	capped, elided := Cap(record, 3, 2, "[0]")
	got := string(capped.Marshal())
	want := `{"desc":"ééé…[+6 chars]","deep":{"a":"<depth-elided>"}}`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if strings.Join(elided, ",") != "[0].desc,[0].deep.a" {
		t.Errorf("elided = %v", elided)
	}
}

func TestSortRecordsTotalOrder(t *testing.T) {
	records := []Value{
		mustParse(t, `{"id":"b","date":"2026-01-01"}`),
		mustParse(t, `{"id":"a","date":"2026-01-01"}`),
		mustParse(t, `{"id":"c"}`),
		mustParse(t, `{"id":"d","date":"2026-02-01"}`),
	}
	SortRecords(records, ParseSort("date desc, id asc"))
	var ids []string
	for _, record := range records {
		value, _ := record.Get("id")
		ids = append(ids, value.Text())
	}
	if strings.Join(ids, "") != "dabc" {
		t.Fatalf("order = %v", ids)
	}
}

func TestNumericSort(t *testing.T) {
	records := []Value{mustParse(t, `{"pos":65535.5}`), mustParse(t, `{"pos":1024}`), mustParse(t, `{"pos":16384}`)}
	SortRecords(records, ParseSort("pos asc"))
	if string(records[0].Marshal()) != `{"pos":1024}` || string(records[2].Marshal()) != `{"pos":65535.5}` {
		t.Fatalf("numeric sort wrong: %s %s", records[0].Marshal(), records[2].Marshal())
	}
}
