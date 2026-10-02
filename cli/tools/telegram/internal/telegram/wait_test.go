package telegram

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/guyravid/ai/cli/tools/telegram/internal/shape"
	"github.com/guyravid/ai/cli/tools/telegram/internal/upstream"
)

// scriptedQueue answers each getUpdates with the next scripted queue and records the requests.
type scriptedQueue struct {
	answers  []string
	requests []*upstream.Request
}

func (s *scriptedQueue) Do(_ context.Context, request *upstream.Request) (shape.Value, error) {
	s.requests = append(s.requests, request)
	answer := "[]"
	if len(s.requests) <= len(s.answers) {
		answer = s.answers[len(s.requests)-1]
	}
	return shape.Parse([]byte(answer))
}

type testClock struct{ now time.Time }

func (c *testClock) Now() time.Time { return c.now }
func (c *testClock) Sleep(_ context.Context, wait time.Duration) error {
	c.now = c.now.Add(wait)
	return nil
}

func waitUpdate(id int, chat int) string {
	return fmt.Sprintf(`{"update_id":%d,"message":{"message_id":%d,"chat":{"id":%d,"type":"private"},"date":1790000000,"text":"hi"}}`, id, id+1000, chat)
}

// The long poll's HTTP deadline is poll + 10s, so the request can outlast the server's wait; the
// plain polls carry no timeout at all.
func TestWaitLongPollRequestOutlastsThePoll(t *testing.T) {
	queue := &scriptedQueue{answers: []string{"[]", "[]", "[" + waitUpdate(5, 111) + "]"}}
	clock := &testClock{now: time.Unix(1790000000, 0)}
	source := &WaitSource{Upstream: queue, Scope: ChatScope{Chat: "111"}, MaxWait: time.Minute, Poll: 2 * time.Second,
		Now: clock.Now, Sleep: clock.Sleep}
	batch, err := source.Fetch(context.Background(), "", 25)
	if err != nil || len(batch.Records) != 1 {
		t.Fatalf("%v %v", batch, err)
	}
	if len(queue.requests) != 3 || queue.requests[0].Timeout != 0 {
		t.Fatalf("requests %+v", queue.requests)
	}
	for _, request := range queue.requests[1:] {
		if request.Body["timeout"] != int64(25) || request.Timeout != 35*time.Second {
			t.Errorf("long poll body %v, deadline %v; want timeout 25 and a 35s deadline", request.Body, request.Timeout)
		}
	}
	for _, request := range queue.requests {
		if _, has := request.Body["offset"]; has {
			t.Error("a wait never sends an offset")
		}
		if request.Write {
			t.Error("a wait is a read")
		}
	}
}

func TestWaitLongPollNeverRunsPastMaxWait(t *testing.T) {
	queue := &scriptedQueue{}
	clock := &testClock{now: time.Unix(1790000000, 0)}
	source := &WaitSource{Upstream: queue, MaxWait: 3 * time.Second, Poll: 2 * time.Second, Now: clock.Now,
		Sleep: clock.Sleep}
	// The fake server does not wait, so advance the clock the way a long poll would.
	advancing := &advancingDoer{inner: queue, clock: clock}
	source.Upstream = advancing
	batch, err := source.Fetch(context.Background(), "", 25)
	if err != nil || len(batch.Records) != 0 || len(batch.Notices) != 1 || batch.Notices[0].Code != "no_reply" {
		t.Fatalf("%+v %v", batch, err)
	}
	for _, request := range queue.requests {
		if seconds, ok := request.Body["timeout"].(int64); ok && seconds > 3 {
			t.Errorf("a %ds long poll is past max-wait", seconds)
		}
	}
	if !strings.Contains(batch.Notices[0].Message, "within 3s") {
		t.Errorf("message %q", batch.Notices[0].Message)
	}
}

type advancingDoer struct {
	inner *scriptedQueue
	clock *testClock
}

func (a *advancingDoer) Do(ctx context.Context, request *upstream.Request) (shape.Value, error) {
	if seconds, ok := request.Body["timeout"].(int64); ok {
		a.clock.now = a.clock.now.Add(time.Duration(seconds) * time.Second)
	}
	return a.inner.Do(ctx, request)
}

func TestChatScopeMatching(t *testing.T) {
	update := flatten(t, `{"update_id":1,"message":{"message_id":2,"chat":{"id":-1001,"type":"supergroup","username":"News_Room"},"date":1790000000}}`)
	cases := []struct {
		name  string
		scope ChatScope
		want  bool
	}{
		{"no narrowing", ChatScope{}, true},
		{"explicit id", ChatScope{Chat: "-1001"}, true},
		{"explicit other id", ChatScope{Chat: "-1002"}, false},
		{"explicit username, any case", ChatScope{Chat: "@news_room"}, true},
		{"allowlist holds the id", ChatScope{Allowed: []string{"5", "-1001"}, FilterAllowed: true}, true},
		{"allowlist holds the username", ChatScope{Allowed: []string{"@NEWS_ROOM"}, FilterAllowed: true}, true},
		{"allowlist without it", ChatScope{Allowed: []string{"5"}, FilterAllowed: true}, false},
		{"empty allowlist keeps nothing", ChatScope{FilterAllowed: true}, false},
		{"explicit chat beats the filter", ChatScope{Chat: "-1001", Allowed: []string{"5"}, FilterAllowed: true}, true},
	}
	for _, tc := range cases {
		if got := tc.scope.Match(update); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}

func TestSentRecordAndAckRecord(t *testing.T) {
	sent := SentRecord(parse(t, `{"message_id":9,"from":{"id":1},"chat":{"id":5,"type":"private","first_name":"G"},"date":1790000000,"text":"hi","entities":[]}`))
	if got := string(sent.Marshal()); got != `{"message_id":9,"date":"2026-09-21T14:13:20Z","chat":{"id":5,"type":"private"},"text":"hi"}` {
		t.Errorf("sent record %s", got)
	}
	if got := string(AckRecord(7, parse(t, `[]`)).Marshal()); got != `{"acked_through":7,"has_more":false}` {
		t.Errorf("ack record %s", got)
	}
	if got := string(AckRecord(7, parse(t, `[{"update_id":9}]`)).Marshal()); got != `{"acked_through":7,"has_more":true}` {
		t.Errorf("ack record %s", got)
	}
}
