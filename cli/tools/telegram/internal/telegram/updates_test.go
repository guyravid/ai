package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/guyravid/ai/cli/tools/telegram/internal/registry"
	"github.com/guyravid/ai/cli/tools/telegram/internal/shape"
	"github.com/guyravid/ai/cli/tools/telegram/internal/upstream"
)

func parse(t *testing.T, text string) shape.Value {
	t.Helper()
	value, err := shape.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func flatten(t *testing.T, text string) Parsed {
	t.Helper()
	parsed, ok := Flatten(parse(t, text))
	if !ok {
		t.Fatalf("not flattened: %s", text)
	}
	return parsed
}

func TestFlattenMessage(t *testing.T) {
	// 1790000000 = 2026-09-21T14:13:20Z
	parsed := flatten(t, `{"update_id":812345,"message":{"message_id":77,"from":{"id":111,"is_bot":false,"first_name":"Guy","username":"guy"},
		"chat":{"id":111,"first_name":"Guy","username":"guy","type":"private"},"date":1790000000,"text":"hello",
		"reply_to_message":{"message_id":70,"text":"earlier"}}}`)
	want := `{"update_id":812345,"type":"message","date":"2026-09-21T14:13:20Z","chat":{"id":111,"type":"private","username":"guy"},` +
		`"from":{"id":111,"username":"guy","first_name":"Guy"},"message_id":77,"text":"hello","reply_to_message_id":70}`
	if got := string(parsed.Record.Marshal()); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if parsed.ID != 812345 || parsed.ChatID != "111" || parsed.ChatUsername != "guy" {
		t.Errorf("parsed %+v", parsed)
	}
}

// Contract §8.5: all times in output are RFC 3339 UTC with a Z suffix.
func TestFlattenDateIsRFC3339UTC(t *testing.T) {
	parsed := flatten(t, `{"update_id":1,"message":{"message_id":1,"chat":{"id":1,"type":"private"},"date":1790000000}}`)
	date, _ := parsed.Record.Get("date")
	if date.Text() != "2026-09-21T14:13:20Z" || !strings.HasSuffix(date.Text(), "Z") {
		t.Errorf("date = %s", date.Raw)
	}
	if _, err := time.Parse(time.RFC3339, date.Text()); err != nil {
		t.Error(err)
	}
	if !parsed.Date.Equal(time.Unix(1790000000, 0)) {
		t.Errorf("parsed date %s", parsed.Date)
	}
}

func TestFlattenTypes(t *testing.T) {
	cases := map[string]string{
		`{"update_id":1,"edited_message":{"message_id":2,"chat":{"id":5,"type":"private"},"date":1790000000,"text":"x"}}`:                           "edited_message",
		`{"update_id":1,"channel_post":{"message_id":2,"chat":{"id":-100123,"type":"channel","title":"News"},"date":1790000000}}`:                   "channel_post",
		`{"update_id":1,"edited_channel_post":{"message_id":2,"chat":{"id":-100123,"type":"channel"},"date":1790000000}}`:                           "edited_channel_post",
		`{"update_id":1,"my_chat_member":{"chat":{"id":9,"type":"private"},"from":{"id":9,"username":"u"},"date":1790000000}}`:                      "my_chat_member",
		`{"update_id":1,"callback_query":{"id":"c","from":{"id":9},"message":{"message_id":3,"chat":{"id":9,"type":"private"},"date":1790000000}}}`: "callback_query",
		`{"update_id":1,"poll":{"id":"p"}}`: "poll",
	}
	for body, kind := range cases {
		parsed := flatten(t, body)
		if got, _ := parsed.Record.Get("type"); got.Text() != kind {
			t.Errorf("type = %s, want %s for %s", got.Raw, kind, body)
		}
	}
	// A channel id beyond 2^53 passes through as written.
	parsed := flatten(t, `{"update_id":1,"message":{"message_id":1,"chat":{"id":-1009007199254740993,"type":"channel"},"date":1790000000}}`)
	if parsed.ChatID != "-1009007199254740993" {
		t.Errorf("chat id %s", parsed.ChatID)
	}
}

func TestFlattenCallbackQueryUsesNestedMessage(t *testing.T) {
	parsed := flatten(t, `{"update_id":1,"callback_query":{"id":"c","from":{"id":9,"username":"u"},"data":"x",
		"message":{"message_id":3,"chat":{"id":9,"type":"private"},"date":1790000000}}}`)
	for key, want := range map[string]string{"message_id": "3", "date": `"2026-09-21T14:13:20Z"`} {
		if got, _ := parsed.Record.Get(key); string(got.Raw) != want {
			t.Errorf("%s = %s, want %s", key, got.Raw, want)
		}
	}
	if from, _ := parsed.Record.Get("from"); !strings.Contains(string(from.Marshal()), `"username":"u"`) {
		t.Errorf("from = %s", from.Marshal())
	}
	if _, ok := parsed.Record.Get("text"); ok {
		t.Error("a callback query has no text")
	}
}

func TestFlattenDocumentAndPhoto(t *testing.T) {
	parsed := flatten(t, `{"update_id":1,"message":{"message_id":1,"chat":{"id":1,"type":"private"},"date":1790000000,"caption":"look",
		"document":{"file_id":"D","file_unique_id":"U","file_name":"report.pdf","mime_type":"application/pdf","file_size":2048},
		"photo":[{"file_id":"small","width":90,"height":60},{"file_id":"large","width":1280,"height":853},{"file_id":"mid","width":320,"height":213}]}}`)
	document, _ := parsed.Record.Get("document")
	if string(document.Marshal()) != `{"file_id":"D","file_unique_id":"U","file_name":"report.pdf","mime_type":"application/pdf","file_size":2048}` {
		t.Errorf("document = %s", document.Marshal())
	}
	if photo, _ := parsed.Record.Get("photo"); photo.Text() != "large" {
		t.Errorf("photo = %s, want the largest size", photo.Raw)
	}
}

func TestFlattenDocumentWithOnlyAFileName(t *testing.T) {
	parsed := flatten(t, `{"update_id":1,"message":{"message_id":1,"chat":{"id":1,"type":"private"},"date":1790000000,"document":{"file_name":"a.txt"}}}`)
	if document, _ := parsed.Record.Get("document"); string(document.Marshal()) != `{"file_name":"a.txt"}` {
		t.Errorf("document = %s", document.Marshal())
	}
	parsed = flatten(t, `{"update_id":1,"message":{"message_id":1,"chat":{"id":1,"type":"private"},"date":1790000000,"document":{}}}`)
	if _, ok := parsed.Record.Get("document"); ok {
		t.Error("an empty document must be omitted")
	}
}

func TestFlattenSkipsWhatIsNotAnUpdate(t *testing.T) {
	if _, ok := Flatten(parse(t, `{"message":{"message_id":1}}`)); ok {
		t.Error("an object without update_id is not an update")
	}
	// A message whose date is zero (inaccessible) has no date.
	parsed := flatten(t, `{"update_id":1,"callback_query":{"id":"c","from":{"id":9},"message":{"message_id":3,"chat":{"id":9,"type":"private"},"date":0}}}`)
	if _, ok := parsed.Record.Get("date"); ok {
		t.Error("date 0 must be omitted")
	}
}

// fakeUpstream returns scripted getUpdates results and records the requests it saw.
type fakeUpstream struct {
	updates  []string
	requests []*upstream.Request
}

func (f *fakeUpstream) Do(_ context.Context, request *upstream.Request) (shape.Value, error) {
	f.requests = append(f.requests, request)
	return shape.Parse([]byte("[" + strings.Join(f.updates, ",") + "]"))
}

func message(id int, chat int, date int64) string {
	return fmt.Sprintf(`{"update_id":%d,"message":{"message_id":%d,"from":{"id":%d,"username":"u%d"},"chat":{"id":%d,"type":"private"},"date":%d,"text":"t%d"}}`,
		id, id, chat, chat, chat, date, id)
}

func ids(records []shape.Value) []int64 {
	var out []int64
	for _, record := range records {
		id, _ := intMember(record, "update_id")
		out = append(out, id)
	}
	return out
}

// Plan, traps 2 and 3: a read sends neither offset nor allowed_updates, only limit.
func TestUpdatesSourceNeverSendsOffsetOrAllowedUpdates(t *testing.T) {
	fake := &fakeUpstream{updates: []string{message(1, 1, 1790000000)}}
	source := &UpdatesSource{Upstream: fake}
	for _, continuation := range []string{"", "5"} {
		if _, err := source.Fetch(context.Background(), continuation, 25); err != nil {
			t.Fatal(err)
		}
	}
	if len(fake.requests) != 1 {
		t.Fatalf("the queue is fetched once per invocation, got %d requests", len(fake.requests))
	}
	request := fake.requests[0]
	encoded, _ := json.Marshal(request.Body)
	if request.Path != "/getUpdates" || request.Write || string(encoded) != `{"limit":100}` {
		t.Errorf("request %s %s write=%v", request.Path, encoded, request.Write)
	}
	if strings.Contains(string(encoded), "offset") || strings.Contains(string(encoded), "allowed_updates") || len(request.Query) != 0 {
		t.Errorf("a read must not send offset or allowed_updates: %s %v", encoded, request.Query)
	}
}

func TestUpdatesSourceFiltersAndOrders(t *testing.T) {
	now := time.Unix(1790000000, 0).UTC()
	fake := &fakeUpstream{updates: []string{
		message(5, 111, now.Unix()), message(3, 222, now.Unix()), message(4, 111, now.Add(-48*time.Hour).Unix()), message(6, 111, now.Unix()),
	}}
	window := &registry.Window{Since: now.Add(-24 * time.Hour), Until: now.Add(time.Minute)}

	all, err := (&UpdatesSource{Upstream: fake, Window: window}).Fetch(context.Background(), "", 25)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(ids(all.Records)) != "[3 5 6]" {
		t.Errorf("window + order: %v", ids(all.Records))
	}
	if all.Total == nil || *all.Total != 3 {
		t.Errorf("total %v", all.Total)
	}

	chat, _ := (&UpdatesSource{Upstream: fake, Window: window, Chat: "111"}).Fetch(context.Background(), "", 25)
	if fmt.Sprint(ids(chat.Records)) != "[5 6]" {
		t.Errorf("chat filter: %v", ids(chat.Records))
	}
	after, _ := (&UpdatesSource{Upstream: fake, Window: window, After: 3}).Fetch(context.Background(), "", 25)
	if fmt.Sprint(ids(after.Records)) != "[5 6]" {
		t.Errorf("after filter: %v", ids(after.Records))
	}
}

func TestUpdatesSourcePagesByLastUpdateID(t *testing.T) {
	now := time.Unix(1790000000, 0)
	fake := &fakeUpstream{updates: []string{message(1, 1, now.Unix()), message(2, 1, now.Unix()), message(3, 1, now.Unix())}}
	source := &UpdatesSource{Upstream: fake}
	first, _ := source.Fetch(context.Background(), "", 2)
	if fmt.Sprint(ids(first.Records)) != "[1 2]" || first.Next != "2" {
		t.Fatalf("first page %v next %q", ids(first.Records), first.Next)
	}
	if source.Resume("", first.Records) != "2" || source.Resume("9", nil) != "9" {
		t.Error("Resume should name the last delivered update_id, or keep the start")
	}
	second, _ := source.Fetch(context.Background(), first.Next, 2)
	if fmt.Sprint(ids(second.Records)) != "[3]" || second.Next != "" {
		t.Errorf("second page %v next %q", ids(second.Records), second.Next)
	}
	if _, err := source.Fetch(context.Background(), "not-a-number", 2); err == nil {
		t.Error("a malformed continuation is usage")
	}
}

func TestUpdatesSourceFilterByUsername(t *testing.T) {
	fake := &fakeUpstream{updates: []string{
		`{"update_id":1,"channel_post":{"message_id":1,"chat":{"id":-100123,"type":"channel","username":"News_Channel"},"date":1790000000}}`,
		message(2, 5, 1790000000),
	}}
	got, _ := (&UpdatesSource{Upstream: fake, Chat: "@news_channel"}).Fetch(context.Background(), "", 25)
	if fmt.Sprint(ids(got.Records)) != "[1]" {
		t.Errorf("username filter is case-insensitive: %v", ids(got.Records))
	}
}

// Plan, trap 4: when 100 updates come back newer ones are invisible, and the tool says so. The total
// is then not exact.
func TestUpdatesSourceWarnsWhenTheQueueWindowIsFull(t *testing.T) {
	var updates []string
	for id := 1; id <= QueueWindow; id++ {
		updates = append(updates, message(id, 1, 1790000000))
	}
	for _, ack := range []bool{false, true} {
		source := &UpdatesSource{Upstream: &fakeUpstream{updates: updates}, AckAvailable: ack}
		batch, err := source.Fetch(context.Background(), "", 25)
		if err != nil {
			t.Fatal(err)
		}
		if len(batch.Notices) != 1 || batch.Notices[0].Code != "queue_window_full" {
			t.Fatalf("notices %+v", batch.Notices)
		}
		message := batch.Notices[0].Message
		if ack != strings.Contains(message, "updates ack --through <update_id> --confirm") {
			t.Errorf("ack=%v message %q", ack, message)
		}
		if batch.Total != nil {
			t.Errorf("with a full window the total is unknown, got %d", *batch.Total)
		}
		// The warning is given once per invocation, not on every page.
		next, _ := source.Fetch(context.Background(), batch.Next, 25)
		if len(next.Notices) != 0 {
			t.Errorf("second page repeated the notice")
		}
	}
	short := &UpdatesSource{Upstream: &fakeUpstream{updates: updates[:QueueWindow-1]}}
	if batch, _ := short.Fetch(context.Background(), "", 25); len(batch.Notices) != 0 || batch.Total == nil {
		t.Errorf("99 updates is not a full window: %+v", batch.Notices)
	}
}
