package app

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClock is the time a wait sees. Sleeping and the server's long polls advance it, so a wait of
// minutes takes no real time and the number of requests it makes is a measure of busy-looping.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(wait time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(wait)
}

func (h *harness) useFakeClock() *fakeClock {
	clock := &fakeClock{now: fixedNow}
	h.app.Now = clock.Now
	h.app.Sleep = func(ctx context.Context, wait time.Duration) error {
		h.sleeps = append(h.sleeps, wait)
		clock.advance(wait)
		return ctx.Err()
	}
	return clock
}

func updatesBody(updates ...string) (int, string) {
	return 200, fmt.Sprintf(`{"ok":true,"result":[%s]}`, strings.Join(updates, ","))
}

// longPolled advances the clock by the long poll the request asked for, as a server that found
// nothing would after waiting that long.
func longPolled(clock *fakeClock, body map[string]any) {
	if seconds, ok := body["timeout"].(float64); ok {
		clock.advance(time.Duration(seconds) * time.Second)
	}
}

func replyUpdate(id, chat, messageID, replyTo int, text string) string {
	return fmt.Sprintf(`{"update_id":%d,"message":{"message_id":%d,"from":{"id":%d,"username":"guy"},"chat":{"id":%d,"type":"private"},"date":%d,"text":%q,"reply_to_message":{"message_id":%d}}}`,
		id, messageID, chat, chat, fixedNow.Unix(), text, replyTo)
}

func waitHarness(t *testing.T) (*harness, *fakeClock) {
	h := newHarness(t)
	h.env["TELEGRAM_DEFAULT_CHAT"] = "111111111"
	return h, h.useFakeClock()
}

func ids(r result) []float64 {
	var got []float64
	for _, record := range r.data() {
		got = append(got, record.(map[string]any)["update_id"].(float64))
	}
	return got
}

func TestWaitLongPollsOnAnEmptyQueueThenReturnsTheMatch(t *testing.T) {
	h, clock := waitHarness(t)
	h.fake.script = func(attempt int, body map[string]any) (int, string) {
		longPolled(clock, body)
		if attempt < 3 {
			return updatesBody()
		}
		return updatesBody(message(5, 111111111, 0, "the reply"))
	}
	result := h.run("updates", "wait", "--after-message", "1000", "--fields", "update_id,text")
	if result.exit != 0 || fmt.Sprint(ids(result)) != "[5]" || len(result.warningCodes()) != 0 {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
	requests := h.fake.all()
	if len(requests) != 3 {
		t.Fatalf("%d requests: %+v", len(requests), requests)
	}
	// The first poll is a plain read; once it found the queue empty the next ones long poll.
	if requests[0].body != `{"limit":100}` || requests[1].body != `{"limit":100,"timeout":25}` || requests[2].body != `{"limit":100,"timeout":25}` {
		t.Errorf("bodies: %s | %s | %s", requests[0].body, requests[1].body, requests[2].body)
	}
	if len(h.sleeps) != 0 {
		t.Errorf("an empty queue is long polled, not slept on: %v", h.sleeps)
	}
}

func TestWaitHTTPDeadlineOutlastsTheLongPoll(t *testing.T) {
	// The per-request deadline is poll + 10s; asserted where the request is built.
	h, clock := waitHarness(t)
	h.env["TELEGRAM_TIMEOUT"] = "5s" // shorter than the long poll: the wait must not inherit it
	h.fake.script = func(attempt int, body map[string]any) (int, string) {
		longPolled(clock, body)
		if attempt < 2 {
			return updatesBody()
		}
		return updatesBody(message(5, 111111111, 0, "late"))
	}
	if result := h.run("updates", "wait"); result.exit != 0 || fmt.Sprint(ids(result)) != "[5]" {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
}

func TestWaitWithNeitherFilterIgnoresWhatWasAlreadyQueued(t *testing.T) {
	h, clock := waitHarness(t)
	old1, old2 := message(1, 111111111, time.Hour, "old"), message(2, 111111111, time.Hour, "older")
	h.fake.script = func(attempt int, body map[string]any) (int, string) {
		longPolled(clock, body)
		if attempt < 3 {
			return updatesBody(old1, old2)
		}
		return updatesBody(old1, old2, message(3, 111111111, 0, "new"))
	}
	result := h.run("updates", "wait", "--fields", "update_id,text")
	if result.exit != 0 || fmt.Sprint(ids(result)) != "[3]" {
		t.Fatalf("snapshot semantics: exit %d: %s", result.exit, result.raw)
	}
	for _, request := range h.fake.all() {
		if strings.Contains(request.body, "timeout") {
			t.Errorf("with updates pending a long poll would return at once, so the wait sleeps instead: %s", request.body)
		}
	}
	if len(h.sleeps) != 2 || h.sleeps[0] != 2*time.Second || h.sleeps[1] != 2*time.Second {
		t.Errorf("sleeps %v, want POLL_INTERVAL twice", h.sleeps)
	}
}

func TestWaitPollsAtTheIntervalNotInABusyLoop(t *testing.T) {
	h, _ := waitHarness(t)
	h.fake.script = func(int, map[string]any) (int, string) {
		return updatesBody(message(1, 111111111, time.Hour, "old and never matching"))
	}
	result := h.run("updates", "wait", "--max-wait", "10s")
	if result.exit != 0 || len(result.data()) != 0 {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
	// 10 fake seconds at one poll per 2 s: polls at 0, 2, 4, 6, 8 and 10.
	if total := h.fake.total(); total < 5 || total > 7 {
		t.Errorf("%d requests in 10s at a 2s interval: a busy loop would make far more", total)
	}
	for _, wait := range h.sleeps {
		if wait != 2*time.Second {
			t.Errorf("slept %v, want POLL_INTERVAL", wait)
		}
	}
}

func TestWaitPollIntervalSettingIsHonoured(t *testing.T) {
	h, _ := waitHarness(t)
	h.env["TELEGRAM_POLL_INTERVAL"] = "5s"
	h.fake.script = func(int, map[string]any) (int, string) { return updatesBody(message(1, 111111111, time.Hour, "old")) }
	h.run("updates", "wait", "--max-wait", "20s")
	if total := h.fake.total(); total < 4 || total > 6 {
		t.Errorf("%d requests in 20s at a 5s interval", total)
	}
}

func TestWaitConflictMidWaitIsReported(t *testing.T) {
	h, _ := waitHarness(t)
	h.fake.script = func(attempt int, body map[string]any) (int, string) {
		if attempt == 1 {
			return updatesBody()
		}
		return 409, `{"ok":false,"error_code":409,"description":"Conflict: terminated by other getUpdates request; make sure that only one bot instance is running"}`
	}
	result := h.run("updates", "wait")
	if result.exit != 7 || result.errorCode() != "conflict" || h.fake.total() != 2 {
		t.Errorf("exit %d after %d requests: %s", result.exit, h.fake.total(), result.raw)
	}
}

func TestWaitStopsWhenTheWindowIsFullAndNothingMatches(t *testing.T) {
	h, _ := waitHarness(t)
	var queue []string
	for id := 1; id <= 100; id++ {
		queue = append(queue, message(id, 111111111, time.Hour, "old"))
	}
	h.fake.script = func(int, map[string]any) (int, string) { return updatesBody(queue...) }
	result := h.run("updates", "wait", "--after-message", "5000")
	if result.exit != 0 || len(result.data()) != 0 || h.fake.total() != 1 || len(h.sleeps) != 0 {
		t.Fatalf("exit %d after %d requests: %s", result.exit, h.fake.total(), result.raw)
	}
	if codes := result.warningCodes(); len(codes) != 1 || codes[0] != "queue_window_full" {
		t.Errorf("warnings %v, want queue_window_full and not no_reply", codes)
	}
}

func TestWaitMatchesOnReplyToAndAfterMessage(t *testing.T) {
	h, _ := waitHarness(t)
	h.fake.script = func(int, map[string]any) (int, string) {
		return updatesBody(
			message(10, 111111111, time.Hour, "unrelated"),
			replyUpdate(11, 111111111, 1011, 4021, "yes"),
			replyUpdate(12, 111111111, 1012, 9999, "reply to something else"),
			replyUpdate(4, 111111111, 1004, 4021, "a reply, but older than the question"),
		)
	}
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--reply-to", "4021"}, "[4 11]"},
		{[]string{"--after-message", "1005"}, "[10 11 12]"},
		{[]string{"--after-message", "1005", "--reply-to", "4021"}, "[11]"},
	}
	for _, tc := range cases {
		result := h.run(append([]string{"updates", "wait", "--fields", "update_id"}, tc.args...)...)
		if result.exit != 0 || fmt.Sprint(ids(result)) != tc.want {
			t.Errorf("%v: got %v (%s), want %s", tc.args, ids(result), result.raw, tc.want)
		}
	}
}

func TestWaitIgnoresWhatIsNotAMessage(t *testing.T) {
	h, _ := waitHarness(t)
	h.fake.script = func(int, map[string]any) (int, string) {
		return updatesBody(`{"update_id":3,"my_chat_member":{"chat":{"id":111111111,"type":"private"},"date":1,"from":{"id":1}}}`,
			message(4, 111111111, 0, "a message"))
	}
	if result := h.run("updates", "wait", "--after-message", "1", "--fields", "update_id"); fmt.Sprint(ids(result)) != "[4]" {
		t.Errorf("only messages and channel posts match: %s", result.raw)
	}
}

func TestWaitAppliesTheAllowlistAsAFilter(t *testing.T) {
	h, clock := waitHarness(t)
	delete(h.env, "TELEGRAM_DEFAULT_CHAT")
	h.env["TELEGRAM_ALLOWED_CHATS"] = "111111111"
	h.fake.script = func(attempt int, body map[string]any) (int, string) {
		longPolled(clock, body)
		if attempt == 1 {
			return updatesBody()
		}
		return updatesBody(message(5, 222222222, 0, "from a chat that is not allowed"), message(6, 111111111, 0, "from an allowed chat"))
	}
	result := h.run("updates", "wait", "--fields", "update_id")
	if result.exit != 0 || fmt.Sprint(ids(result)) != "[6]" {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
	if codes := result.warningCodes(); len(codes) != 1 || codes[0] != "filtered_chats" || strings.Contains(result.raw, "222222222") {
		t.Errorf("warnings %v: %s", codes, result.raw)
	}
}

func TestWaitListensToTheDefaultChatWhenThereIsOne(t *testing.T) {
	h, _ := waitHarness(t)
	h.env["TELEGRAM_ALLOWED_CHATS"] = "222222222"
	h.fake.script = func(int, map[string]any) (int, string) {
		return updatesBody(message(5, 222222222, 0, "an allowed chat, but not the one being waited on"), message(6, 111111111, 0, "default"))
	}
	result := h.run("updates", "wait", "--after-message", "1", "--fields", "update_id")
	if fmt.Sprint(ids(result)) != "[6]" || len(result.warningCodes()) != 0 {
		t.Errorf("%s", result.raw)
	}
	if other := h.run("updates", "wait", "--chat", "222222222", "--after-message", "1", "--fields", "update_id"); fmt.Sprint(ids(other)) != "[5]" {
		t.Errorf("--chat overrides the default: %s", other.raw)
	}
}

func TestWaitWithNoMatchIsASuccessWithANoReplyWarning(t *testing.T) {
	h, clock := waitHarness(t)
	h.fake.script = func(_ int, body map[string]any) (int, string) {
		longPolled(clock, body)
		return updatesBody()
	}
	result := h.run("updates", "wait")
	if result.exit != 0 || result.doc["ok"] != true || result.doc["data"] == nil || len(result.data()) != 0 {
		t.Fatalf("an empty wait is a success: exit %d: %s", result.exit, result.raw)
	}
	if codes := result.warningCodes(); len(codes) != 1 || codes[0] != "no_reply" || !strings.Contains(result.raw, "No matching message within 1m0s.") {
		t.Errorf("warnings %v: %s", codes, result.raw)
	}
	// 60s as one plain poll, 25s, 25s and a final 10s that ends exactly at the deadline.
	requests := h.fake.all()
	if len(requests) != 4 || requests[3].body != `{"limit":100,"timeout":10}` {
		t.Errorf("%d requests, last %q: a long poll never runs past --max-wait", len(requests), requests[len(requests)-1].body)
	}
}

func TestWaitBudgetDefaultsAndValidation(t *testing.T) {
	h, _ := waitHarness(t)
	for name, args := range map[string][]string{
		"budget below max-wait":    {"updates", "wait", "--budget", "10s", "--max-wait", "60s"},
		"budget below the default": {"updates", "wait", "--budget", "30s"},
		"max-wait over an hour":    {"updates", "wait", "--max-wait", "2h"},
		"max-wait without units":   {"updates", "wait", "--max-wait", "60"},
		"max-wait of zero":         {"updates", "wait", "--max-wait", "0s"},
	} {
		if result := h.run(args...); result.exit != 2 || result.errorCode() != "usage" {
			t.Errorf("%s: exit %d: %s", name, result.exit, result.raw)
		}
	}
	if h.fake.total() != 0 {
		t.Errorf("%d requests for usage errors", h.fake.total())
	}
	h.fake.script = func(int, map[string]any) (int, string) { return updatesBody(message(5, 111111111, 0, "x")) }
	for _, args := range [][]string{
		{"updates", "wait", "--max-wait", "1h"},                     // the default budget grows with it
		{"updates", "wait", "--budget", "90s", "--max-wait", "90s"}, // equal is allowed
	} {
		if result := h.run(args...); result.exit != 0 {
			t.Errorf("%v: exit %d: %s", args, result.exit, result.raw)
		}
	}
}

func TestWaitCursorContinuesWithoutWaiting(t *testing.T) {
	h, _ := waitHarness(t)
	h.fake.script = func(int, map[string]any) (int, string) {
		return updatesBody(message(20, 111111111, 0, "a"), message(21, 111111111, 0, "b"))
	}
	first := h.run("updates", "wait", "--after-message", "1", "--limit", "1", "--fields", "update_id")
	cursor, _ := first.page()["next_cursor"].(string)
	if fmt.Sprint(ids(first)) != "[20]" || cursor == "" {
		t.Fatalf("%s", first.raw)
	}
	rest := h.run("updates", "wait", "--cursor", cursor)
	if rest.exit != 0 || fmt.Sprint(ids(rest)) != "[21]" || len(h.sleeps) != 0 || h.fake.total() != 2 {
		t.Errorf("exit %d after %d requests: %s", rest.exit, h.fake.total(), rest.raw)
	}
}

func TestWaitDryRunSendsNothing(t *testing.T) {
	h, _ := waitHarness(t)
	result := h.run("updates", "wait", "--dry-run")
	if result.exit != 0 || result.meta()["dry_run"] != true || result.preview() == nil || h.fake.total() != 0 {
		t.Errorf("exit %d after %d requests: %s", result.exit, h.fake.total(), result.raw)
	}
}

func TestInterruptDuringAWaitIsCanceled(t *testing.T) {
	h, _ := waitHarness(t)
	h.fake.script = func(int, map[string]any) (int, string) { return updatesBody(message(1, 111111111, time.Hour, "old")) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.app.Sleep = func(ctx context.Context, _ time.Duration) error {
		cancel() // SIGINT arrives while the wait is sleeping between polls
		return ctx.Err()
	}
	var stdout bytes.Buffer
	exit := h.app.Run(ctx, []string{"updates", "wait"}, &stdout, func() int {
		if ctx.Err() != nil {
			return 130
		}
		return 0
	})
	if exit != 130 || !strings.Contains(stdout.String(), `"code":"canceled"`) || !strings.Contains(stdout.String(), `"command":"updates.wait"`) {
		t.Errorf("exit %d: %s", exit, stdout.String())
	}
	if h.fake.total() != 1 {
		t.Errorf("%d requests: a canceled wait stops at once", h.fake.total())
	}
}

// The orientation and the topics teach the send, then wait flow, with a worked example, in the full
// build only (a read-only build has no send to teach).
func TestDelegationAndUploadsTopicsTeachTheFlow(t *testing.T) {
	h := newHarness(t)
	delegation := h.run("teach", "delegation").raw
	if !writesAvailable(h) {
		if !strings.Contains(delegation, `"code":"usage"`) {
			t.Errorf("the read-only build has no delegation topic: %s", delegation)
		}
		return
	}
	for _, want := range []string{"messages send", "updates wait --after-message", "message_id", "no_reply", "Worked example"} {
		if !strings.Contains(delegation, want) {
			t.Errorf("delegation lacks %q", want)
		}
	}
	uploads := h.run("teach", "uploads").raw
	for _, want := range []string{"50 MB", "10 MB", "bot<redacted>", "read-only build", "--allow-any-chat"} {
		if !strings.Contains(uploads, want) {
			t.Errorf("uploads lacks %q", want)
		}
	}
	updates := h.run("teach", "updates").raw
	for _, want := range []string{"updates ack", "permanently deletes", "no_reply", "--max-wait", "POLL_INTERVAL"} {
		if !strings.Contains(updates, want) {
			t.Errorf("updates lacks %q", want)
		}
	}
	chats := h.run("teach", "chats").raw
	for _, want := range []string{"chat_not_allowed", "filtered_chats", "no_allowed_chats", "--allow-any-chat"} {
		if !strings.Contains(chats, want) {
			t.Errorf("chats lacks %q", want)
		}
	}
	orientation := h.run("teach").raw
	for _, want := range []string{"PR-assistant bot", "--confirm", "updates ack", "permanently deletes", "messages send"} {
		if !strings.Contains(orientation, want) {
			t.Errorf("orientation lacks %q", want)
		}
	}
	if len(orientation) >= 4096 {
		t.Errorf("orientation is %d bytes", len(orientation))
	}
}

func writesAvailable(h *harness) bool { return h.app.Registry.Get("messages.send") != nil }
