package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guyravid/ai/cli/tools/telegram/internal/commands"
	"github.com/guyravid/ai/cli/tools/telegram/internal/registry"
)

// Fake tokens only. They are written to temp files at mode 0600 or put in a test environment; no
// test reads a real token or calls the real API.
const (
	testToken    = "123456789:AAFakeBotTokenForUnitTests0123456789ab"
	profileToken = "987654321:AAFakeBuildsTokenForUnitTests012345678"
)

var fixedNow = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

// seen is one request the fake received.
type seen struct {
	token  string // the path segment after /bot
	method string // the Bot API method
	body   string
	query  string
	header http.Header
}

// fakeTelegram is a request-recording stand-in for api.telegram.org.
type fakeTelegram struct {
	mu       sync.Mutex
	requests []seen
	updates  []string // raw update objects returned by getUpdates
	// override answers a method itself: status, body, and the attempt number (1-based) per method.
	override map[string]func(attempt int) (int, string)
	attempts map[string]int
	chats    map[string]string // known chats: id -> getChat result
	// script answers getUpdates itself when set: the attempt number (1-based) and the request body.
	script func(attempt int, body map[string]any) (int, string)
}

func newFake() *fakeTelegram {
	return &fakeTelegram{
		override: map[string]func(int) (int, string){},
		attempts: map[string]int{},
		chats: map[string]string{
			"111111111":      `{"id":111111111,"type":"private","first_name":"Guy","username":"guy"}`,
			"-1001234567890": `{"id":-1001234567890,"type":"supergroup","title":"Family"}`,
		},
	}
}

func (f *fakeTelegram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	path := strings.TrimPrefix(r.URL.Path, "/bot")
	token, method, _ := strings.Cut(path, "/")
	f.mu.Lock()
	f.requests = append(f.requests, seen{token: token, method: method, body: string(body), query: r.URL.RawQuery, header: r.Header.Clone()})
	f.attempts[method]++
	attempt := f.attempts[method]
	override := f.override[method]
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Date", fixedNow.Format(http.TimeFormat))
	if override != nil {
		status, payload := override(attempt)
		w.WriteHeader(status)
		fmt.Fprint(w, payload)
		return
	}
	if method == "getUpdates" && f.script != nil {
		var request map[string]any
		_ = json.Unmarshal(body, &request)
		status, payload := f.script(attempt, request)
		w.WriteHeader(status)
		fmt.Fprint(w, payload)
		return
	}
	switch method {
	case "sendMessage", "sendDocument", "sendPhoto":
		// A sent message: the id is the attempt number, so tests can tell sends apart.
		fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d,"from":{"id":123456789,"is_bot":true,"username":"test_bot"},"chat":{"id":111111111,"type":"private","first_name":"Guy"},"date":%d,"text":"echo"}}`,
			4000+attempt, fixedNow.Unix())
	case "getMe":
		fmt.Fprint(w, `{"ok":true,"result":{"id":123456789,"is_bot":true,"first_name":"Test Bot","username":"test_bot","can_join_groups":true,"can_read_all_group_messages":false,"supports_inline_queries":false}}`)
	case "getChat":
		var request map[string]any
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.UseNumber()
		_ = decoder.Decode(&request)
		id := fmt.Sprint(request["chat_id"])
		if result, ok := f.chats[id]; ok {
			fmt.Fprintf(w, `{"ok":true,"result":%s}`, result)
			return
		}
		w.WriteHeader(400)
		fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`)
	case "getUpdates":
		fmt.Fprintf(w, `{"ok":true,"result":[%s]}`, strings.Join(f.updates, ","))
	default:
		w.WriteHeader(404)
		fmt.Fprint(w, `{"ok":false,"error_code":404,"description":"Not Found"}`)
	}
}

func (f *fakeTelegram) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeTelegram) count(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, request := range f.requests {
		if request.method == method {
			count++
		}
	}
	return count
}

func (f *fakeTelegram) all() []seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]seen(nil), f.requests...)
}

// message builds one message update at a time relative to fixedNow.
func message(id, chat int, age time.Duration, text string) string {
	return fmt.Sprintf(`{"update_id":%d,"message":{"message_id":%d,"from":{"id":%d,"is_bot":false,"first_name":"Guy","username":"guy"},"chat":{"id":%d,"type":"private","username":"guy"},"date":%d,"text":%q}}`,
		id, id+1000, chat, chat, fixedNow.Add(-age).Unix(), text)
}

type harness struct {
	t      *testing.T
	fake   *fakeTelegram
	server *httptest.Server
	app    *App
	env    map[string]string
	home   string
	sleeps []time.Duration
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	fake := newFake()
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	home := t.TempDir()
	env := map[string]string{
		"HOME":               home,
		"XDG_CONFIG_HOME":    filepath.Join(home, "config"),
		"XDG_CACHE_HOME":     filepath.Join(home, "cache"),
		"TELEGRAM_BASE_URL":  server.URL,
		"TELEGRAM_BOT_TOKEN": testToken,
		// The chats the older tests address. Tests of the allowlist itself set their own.
		"TELEGRAM_ALLOWED_CHATS": "111111111,222222222,111,424242,-1001234567890",
	}
	all := append(registry.Builtins(), commands.Reads()...)
	all = append(all, commands.Writes()...)
	reg := registry.New(all)
	if err := reg.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := reg.ValidateMutating(commands.MutatingNames); err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, fake: fake, server: server, env: env, home: home}
	h.app = &App{
		Build: Build{Tool: "telegram", Prefix: "TELEGRAM", Version: "test", WritesEnabled: commands.WritesEnabled,
			GOOS: "linux", GOARCH: "arm64"},
		Registry: reg,
		Env:      func(name string) (string, bool) { value, ok := h.env[name]; return value, ok },
		Environ: func() []string {
			var pairs []string
			for key, value := range h.env {
				pairs = append(pairs, key+"="+value)
			}
			return pairs
		},
		Now:           func() time.Time { return fixedNow },
		MutatingNames: commands.MutatingNames,
		Sleep: func(_ context.Context, wait time.Duration) error {
			h.sleeps = append(h.sleeps, wait)
			return nil
		},
	}
	return h
}

type result struct {
	exit   int
	raw    string
	stderr string
	doc    map[string]any
}

func (h *harness) run(args ...string) result {
	h.t.Helper()
	var stdout, stderr bytes.Buffer
	h.app.Stderr = &stderr
	exit := h.app.Run(context.Background(), args, &stdout, func() int { return 0 })
	out := result{exit: exit, raw: stdout.String(), stderr: stderr.String()}
	if strings.HasPrefix(out.raw, "{") {
		if err := json.Unmarshal(stdout.Bytes(), &out.doc); err != nil {
			h.t.Fatalf("invalid JSON from %v: %v\n%s", args, err, out.raw)
		}
		if strings.Count(strings.TrimRight(out.raw, "\n"), "\n") > 0 && !strings.Contains(strings.Join(args, " "), "--pretty") {
			h.t.Fatalf("more than one line of output from %v", args)
		}
	}
	return out
}

func (r result) meta() map[string]any {
	meta, _ := r.doc["meta"].(map[string]any)
	return meta
}

func (r result) page() map[string]any {
	page, _ := r.meta()["page"].(map[string]any)
	return page
}

func (r result) data() []any {
	items, _ := r.doc["data"].([]any)
	return items
}

func (r result) object() map[string]any {
	object, _ := r.doc["data"].(map[string]any)
	return object
}

func (r result) errorBody() map[string]any {
	body, _ := r.doc["error"].(map[string]any)
	return body
}

func (r result) errorCode() string {
	code, _ := r.errorBody()["code"].(string)
	return code
}

func (r result) warningCodes() []string {
	var codes []string
	warnings, _ := r.meta()["warnings"].([]any)
	for _, warning := range warnings {
		codes = append(codes, warning.(map[string]any)["code"].(string))
	}
	return codes
}

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeTokenFile writes a fake token to a temp file at mode 0600.
func writeTokenFile(t *testing.T, token string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBotGetReturnsTheBot(t *testing.T) {
	h := newHarness(t)
	result := h.run("bot", "get")
	if result.exit != 0 {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
	data := result.object()
	if data["username"] != "test_bot" || data["id"] != float64(123456789) || data["can_join_groups"] != true {
		t.Errorf("data %v", data)
	}
	if data["can_read_all_group_messages"] != false {
		t.Errorf("false is a value, not an absence: %v", data)
	}
	requests := h.fake.all()
	if len(requests) != 1 || requests[0].method != "getMe" || requests[0].token != testToken {
		t.Fatalf("requests %+v", requests)
	}
	for name := range requests[0].header {
		if strings.EqualFold(name, "Authorization") {
			t.Error("the token must travel in the URL path only, never a header")
		}
	}
}

func TestChatsGetUsesDefaultChatSettingAndChatFlag(t *testing.T) {
	h := newHarness(t)
	if result := h.run("chats", "get"); result.exit != 2 || result.errorCode() != "usage" || h.fake.total() != 0 {
		t.Fatalf("no chat anywhere is usage with no request: exit %d %s", result.exit, result.raw)
	}
	h.env["TELEGRAM_DEFAULT_CHAT"] = "111111111"
	result := h.run("chats", "get")
	if result.exit != 0 || result.object()["username"] != "guy" || result.object()["type"] != "private" {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
	if body := h.fake.all()[0].body; body != `{"chat_id":111111111}` {
		t.Errorf("numeric ids go upstream as numbers: %s", body)
	}
	flagged := h.run("chats", "get", "--chat", "-1001234567890", "--fields", "id,title")
	if flagged.object()["title"] != "Family" {
		t.Errorf("--chat should win over default_chat: %s", flagged.raw)
	}
	for _, bad := range []string{"abc", "12 34", "@x"} {
		if result := h.run("chats", "get", "--chat", bad); result.exit != 2 {
			t.Errorf("--chat %q: exit %d, want usage", bad, result.exit)
		}
	}
}

func TestUpdatesListFlattensAndConvertsDates(t *testing.T) {
	h := newHarness(t)
	h.fake.updates = []string{message(7, 111111111, 2*time.Hour, "second"), message(5, 111111111, 3*time.Hour, "first"),
		message(9, 222222222, 49*time.Hour, "too old for the default window")}
	result := h.run("updates", "list")
	if result.exit != 0 {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
	records := result.data()
	if len(records) != 2 {
		t.Fatalf("got %d records: %s", len(records), result.raw)
	}
	first := records[0].(map[string]any)
	// 3 hours before 2026-09-30T12:00:00Z.
	if first["update_id"] != float64(5) || first["date"] != "2026-09-30T09:00:00Z" || first["text"] != "first" || first["message_id"] != float64(1005) {
		t.Errorf("first record %v", first)
	}
	if records[1].(map[string]any)["update_id"] != float64(7) {
		t.Errorf("update_id asc: %s", result.raw)
	}
	// The default fields: update_id,type,date,chat.id,from.username,message_id,text.
	for _, key := range []string{"update_id", "type", "date", "chat", "from", "message_id", "text"} {
		if _, ok := first[key]; !ok {
			t.Errorf("default field %s missing: %v", key, first)
		}
	}
	if chat := first["chat"].(map[string]any); len(chat) != 1 || chat["id"] != float64(111111111) {
		t.Errorf("only chat.id by default: %v", chat)
	}
	window, _ := result.meta()["window"].(map[string]any)
	if window["source"] != "default" || window["since"] != "2026-09-29T12:00:00Z" || window["until"] != "2026-09-30T12:00:00Z" {
		t.Errorf("window %v", window)
	}
	if result.meta()["sort"] != "update_id asc" || result.page()["total"] != float64(2) || result.page()["total_is_exact"] != true {
		t.Errorf("meta %v", result.meta())
	}
	// RFC 3339 UTC, parseable.
	if parsed, err := time.Parse(time.RFC3339, first["date"].(string)); err != nil || !strings.HasSuffix(first["date"].(string), "Z") || parsed.IsZero() {
		t.Errorf("date %v", first["date"])
	}

	wide := h.run("updates", "list", "--since", "-3d", "--fields", "update_id,chat.id,chat.type,from.username,text", "--chat", "222222222")
	if len(wide.data()) != 1 || wide.data()[0].(map[string]any)["text"] != "too old for the default window" {
		t.Errorf("--since and --chat: %s", wide.raw)
	}
	if source := wide.meta()["window"].(map[string]any)["source"]; source != "flag" {
		t.Errorf("window source %v", source)
	}
}

func TestUpdatesListEmptyQueueIsSuccess(t *testing.T) {
	h := newHarness(t)
	result := h.run("updates", "list")
	if result.exit != 0 || result.doc["data"] == nil || len(result.data()) != 0 || result.page()["count"] != float64(0) {
		t.Errorf("an empty queue is a success: %s", result.raw)
	}
}

// Plan, traps 2 and 3: nothing a read sends changes the bot's queue or subscription.
func TestNoReadSendsOffsetOrAllowedUpdates(t *testing.T) {
	h := newHarness(t)
	h.env["TELEGRAM_DEFAULT_CHAT"] = "111111111"
	for id := 1; id <= 4; id++ {
		h.fake.updates = append(h.fake.updates, message(id, 111111111, time.Hour, "m"))
	}
	first := h.run("updates", "list", "--limit", "2", "--after", "1")
	cursor, _ := first.page()["next_cursor"].(string)
	if cursor == "" {
		t.Fatalf("expected a next_cursor: %s", first.raw)
	}
	runs := [][]string{
		{"bot", "get"}, {"chats", "get"}, {"doctor"}, {"updates", "list"}, {"updates", "list", "--chat", "111111111"},
		{"updates", "list", "--cursor", cursor}, {"updates", "list", "--dry-run"},
	}
	for _, args := range runs {
		h.run(args...)
	}
	for _, request := range h.fake.all() {
		for _, banned := range []string{"offset", "allowed_updates"} {
			if strings.Contains(request.body, banned) || strings.Contains(request.query, banned) {
				t.Errorf("%s carried %s: body %q query %q", request.method, banned, request.body, request.query)
			}
		}
		if request.method == "getUpdates" && request.body != `{"limit":100}` {
			t.Errorf("getUpdates body %q, want only the limit", request.body)
		}
	}
	if h.fake.count("getUpdates") == 0 {
		t.Fatal("no getUpdates was made")
	}
}

func TestUpdatesListCursorCarriesIntegerParameters(t *testing.T) {
	h := newHarness(t)
	for id := 1; id <= 6; id++ {
		h.fake.updates = append(h.fake.updates, message(id, 111111111, time.Hour, fmt.Sprintf("m%d", id)))
	}
	var got []float64
	args := []string{"updates", "list", "--after", "1", "--limit", "2", "--chat", "111111111", "--fields", "update_id"}
	for step := 0; step < 5; step++ {
		result := h.run(args...)
		if result.exit != 0 {
			t.Fatalf("step %d exit %d: %s", step, result.exit, result.raw)
		}
		for _, record := range result.data() {
			got = append(got, record.(map[string]any)["update_id"].(float64))
		}
		if result.page()["has_more"] != true {
			break
		}
		args = []string{"updates", "list", "--cursor", result.page()["next_cursor"].(string)}
	}
	if fmt.Sprint(got) != "[2 3 4 5 6]" {
		t.Errorf("walked %v, want 2..6 (after=1 survives the cursor)", got)
	}
}

// Plan, trap 4.
func TestFullQueueWindowWarns(t *testing.T) {
	h := newHarness(t)
	for id := 1; id <= 100; id++ {
		h.fake.updates = append(h.fake.updates, message(id, 111111111, time.Hour, "m"))
	}
	result := h.run("updates", "list", "--limit", "5")
	if result.exit != 0 || len(result.data()) != 5 {
		t.Fatalf("exit %d: %.300s", result.exit, result.raw)
	}
	if codes := result.warningCodes(); len(codes) != 1 || codes[0] != "queue_window_full" {
		t.Errorf("warnings %v", codes)
	}
	if result.page()["total"] != nil || result.page()["total_is_exact"] != false {
		t.Errorf("the total of a full window is unknown: %v", result.page())
	}
	h.fake.updates = h.fake.updates[:99]
	if codes := h.run("updates", "list").warningCodes(); len(codes) != 0 {
		t.Errorf("99 updates is not a full window: %v", codes)
	}
}

func TestErrorsMapToContractCodes(t *testing.T) {
	cases := []struct {
		name     string
		method   string
		args     []string
		status   int
		body     string
		exit     int
		code     string
		requests int
	}{
		{"403 is chat_unreachable", "getChat", []string{"chats", "get", "--chat", "111111111"}, 403,
			`{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`, 32, "chat_unreachable", 1},
		{"409 is conflict", "getUpdates", []string{"updates", "list"}, 409,
			`{"ok":false,"error_code":409,"description":"Conflict: terminated by other getUpdates request; make sure that only one bot instance is running"}`, 7, "conflict", 1},
		{"chat not found is not_found", "getChat", []string{"chats", "get", "--chat", "424242"}, 400,
			`{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`, 5, "not_found", 1},
		{"401 is auth", "getMe", []string{"bot", "get"}, 401, `{"ok":false,"error_code":401,"description":"Unauthorized"}`, 4, "auth", 1},
		{"other 400 is validation", "getChat", []string{"chats", "get", "--chat", "111111111"}, 400,
			`{"ok":false,"error_code":400,"description":"Bad Request: wrong parameter"}`, 6, "validation", 1},
		{"5xx is upstream, retried to the limit", "getMe", []string{"bot", "get"}, 502, `{"ok":false,"error_code":502,"description":"Bad Gateway"}`, 12, "upstream", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.fake.override[tc.method] = func(int) (int, string) { return tc.status, tc.body }
			result := h.run(tc.args...)
			if result.exit != tc.exit || result.errorCode() != tc.code {
				t.Fatalf("exit %d code %s, want %d %s: %s", result.exit, result.errorCode(), tc.exit, tc.code, result.raw)
			}
			if got := result.errorBody()["exit_code"]; got != float64(tc.exit) {
				t.Errorf("error.exit_code %v", got)
			}
			if h.fake.total() != tc.requests {
				t.Errorf("%d requests, want %d", h.fake.total(), tc.requests)
			}
		})
	}
}

func TestChatUnreachableIsNotRetriableAndSaysWhatToDo(t *testing.T) {
	h := newHarness(t)
	h.fake.override["getChat"] = func(int) (int, string) {
		return 403, `{"ok":false,"error_code":403,"description":"Forbidden: bot can't initiate conversation with a user"}`
	}
	result := h.run("chats", "get", "--chat", "111111111")
	body := result.errorBody()
	if body["retriable"] != false || !strings.Contains(fmt.Sprint(body["hint"]), "Start") {
		t.Errorf("error %v", body)
	}
	if details := body["details"].(map[string]any); details["upstream_status"] != float64(403) {
		t.Errorf("details %v", details)
	}
}

func TestRateLimitedReadRetriesOnceThenReportsRetryAfter(t *testing.T) {
	h := newHarness(t)
	h.fake.override["getMe"] = func(int) (int, string) {
		return 429, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 1","parameters":{"retry_after":1}}`
	}
	result := h.run("bot", "get")
	if result.exit != 9 || result.errorCode() != "rate_limited" {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
	body := result.errorBody()
	if body["retry_after_ms"] != float64(1000) || body["retriable"] != true {
		t.Errorf("error %v", body)
	}
	if h.fake.count("getMe") != 2 || len(h.sleeps) != 1 || h.sleeps[0] != time.Second {
		t.Errorf("%d requests, waits %v: want one retry after waiting retry_after", h.fake.count("getMe"), h.sleeps)
	}
}

func TestRateLimitedReadSucceedsOnRetry(t *testing.T) {
	h := newHarness(t)
	h.fake.override["getMe"] = func(attempt int) (int, string) {
		if attempt == 1 {
			return 429, `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":2}}`
		}
		return 200, `{"ok":true,"result":{"id":1,"username":"test_bot"}}`
	}
	result := h.run("bot", "get")
	if result.exit != 0 || result.object()["username"] != "test_bot" || fmt.Sprint(h.sleeps) != "[2s]" {
		t.Errorf("exit %d waits %v: %s", result.exit, h.sleeps, result.raw)
	}
}

// Plan, trap 1: the token is in the URL, and Go's transport errors embed the URL.
func TestDialErrorNeverLeaksTheToken(t *testing.T) {
	h := newHarness(t)
	dead := httptest.NewServer(http.NotFoundHandler())
	h.env["TELEGRAM_BASE_URL"] = dead.URL
	dead.Close() // nothing listens: the dial fails
	for _, args := range [][]string{
		{"bot", "get"}, {"updates", "list", "--verbose"}, {"bot", "get", "--verbose", "--pretty"}, {"doctor", "--verbose"}, {"bot", "get", "--human"},
	} {
		result := h.run(args...)
		if args[0] == "bot" && result.exit != 11 || result.doc != nil && args[0] == "bot" && result.errorCode() != "network" {
			t.Fatalf("%v: exit %d: %s", args, result.exit, result.raw)
		}
		for _, text := range []string{result.raw, result.stderr} {
			for _, secret := range []string{testToken, testToken[:12], "AAFakeBotToken", testToken[10:]} {
				if strings.Contains(text, secret) {
					t.Errorf("%v: %q leaked in\n%s", args, secret, text)
				}
			}
		}
	}
	verbose := h.run("bot", "get", "--verbose")
	if !strings.Contains(verbose.stderr, "upstream request") || !strings.Contains(verbose.stderr, "redacted") {
		t.Errorf("--verbose should log the request with the token masked: %q", verbose.stderr)
	}
}

// The token is also kept out of a dry-run preview and out of anything an upstream error echoes.
func TestUpstreamEchoOfTheTokenIsRedacted(t *testing.T) {
	h := newHarness(t)
	h.fake.override["getMe"] = func(int) (int, string) {
		return 400, fmt.Sprintf(`{"ok":false,"error_code":400,"description":"Bad Request: bad token /bot%s/getMe"}`, testToken)
	}
	result := h.run("bot", "get")
	if result.exit != 6 || strings.Contains(result.raw, testToken) || strings.Contains(result.raw, testToken[:12]) {
		t.Errorf("exit %d: %s", result.exit, result.raw)
	}
	dry := h.run("updates", "list", "--dry-run")
	if dry.exit != 0 || strings.Contains(dry.raw, testToken[:12]) || !strings.Contains(dry.raw, "bot<redacted>/getUpdates") {
		t.Errorf("dry run: %s", dry.raw)
	}
	if dry.meta()["dry_run"] != true {
		t.Errorf("dry_run not set: %s", dry.raw)
	}
}

func TestMissingTokenIsAuthWithHint(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "TELEGRAM_BOT_TOKEN")
	result := h.run("bot", "get")
	if result.exit != 4 || result.errorCode() != "auth" || !strings.Contains(fmt.Sprint(result.errorBody()["hint"]), "TELEGRAM_BOT_TOKEN_FILE") || h.fake.total() != 0 {
		t.Errorf("exit %d: %s", result.exit, result.raw)
	}
	if dry := h.run("bot", "get", "--dry-run"); dry.exit != 0 {
		t.Errorf("a dry run needs no token: %s", dry.raw)
	}
}

func TestMalformedTokenIsConfigAndNeverEchoed(t *testing.T) {
	h := newHarness(t)
	bad := "not a token/with?odd#chars"
	h.env["TELEGRAM_BOT_TOKEN"] = bad
	result := h.run("bot", "get")
	if result.exit != 3 || result.errorCode() != "config" || strings.Contains(result.raw, "odd#chars") || h.fake.total() != 0 {
		t.Errorf("exit %d: %s", result.exit, result.raw)
	}
}

func TestSecretOnArgvIsRefused(t *testing.T) {
	h := newHarness(t)
	result := h.run("bot", "get", "--token", testToken)
	details, _ := result.errorBody()["details"].(map[string]any)
	if result.exit != 8 || details["reason"] != "secret_on_argv" || strings.Contains(result.raw, testToken) || h.fake.total() != 0 {
		t.Errorf("exit %d: %s", result.exit, result.raw)
	}
	if !strings.Contains(fmt.Sprint(result.errorBody()["hint"]), "TELEGRAM_BOT_TOKEN_FILE") {
		t.Errorf("hint %v", result.errorBody()["hint"])
	}
}

func TestBotTokenOnArgvIsRefusedEverywhere(t *testing.T) {
	cases := map[string][]string{
		"separate":  {"bot", "get", "--bot-token", testToken},
		"equals":    {"bot", "get", "--bot-token=" + testToken},
		"domain":    {"messages", "send", "--text", "x", "--bot-token", testToken},
		"builtin":   {"tools", "--bot-token", testToken},
		"first arg": {"--bot-token=" + testToken, "bot", "get"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			result := h.run(args...)
			details, _ := result.errorBody()["details"].(map[string]any)
			if result.exit != 8 || result.errorCode() != "refused" || details["reason"] != "secret_on_argv" || details["flag"] != "--bot-token" {
				t.Errorf("exit %d: %s", result.exit, result.raw)
			}
			if strings.Contains(result.raw, testToken) || strings.Contains(result.stderr, testToken) || h.fake.total() != 0 {
				t.Errorf("token echoed or call made: %s / %s", result.raw, result.stderr)
			}
			if hint := fmt.Sprint(result.errorBody()["hint"]); !strings.Contains(hint, "@BotFather") || !strings.Contains(hint, "TELEGRAM_BOT_TOKEN_FILE") {
				t.Errorf("hint %q", hint)
			}
		})
	}
}

// profileSetup writes a config with a default bot and a "builds" profile, and returns its path.
func profileSetup(t *testing.T, buildsMembers string) string {
	t.Helper()
	return writeConfigFile(t, fmt.Sprintf(`{"config_version":1,
		"default":{"description":"PR-assistant bot.","default_chat":"111111111","allowed_chats":"111111111,222222222"},
		"profiles":{"builds":{"description":"CI bot.",%s}}}`, buildsMembers))
}

func TestProfileUsesItsOwnTokenAndInheritsChatSettings(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "TELEGRAM_BOT_TOKEN")
	h.env["TELEGRAM_BOT_TOKEN_FILE"] = writeTokenFile(t, testToken)
	path := profileSetup(t, fmt.Sprintf(`"bot_token_file":%q`, writeTokenFile(t, profileToken)))
	// An default-scope TELEGRAM_BOT_TOKEN_FILE is set: the profile still uses its own file.
	result := h.run("chats", "get", "--profile", "builds", "--config", path)
	if result.exit != 0 || result.object()["id"] != float64(111111111) {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
	requests := h.fake.all()
	if len(requests) != 1 || requests[0].token != profileToken || requests[0].body != `{"chat_id":111111111}` {
		t.Errorf("the profile's token and the inherited default_chat should be used: %+v", requests)
	}
	// The default bot, without --profile, uses its own source.
	h.env["TELEGRAM_BOT_TOKEN"] = testToken
	if h.run("bot", "get", "--config", path); h.fake.all()[1].token != testToken {
		t.Errorf("without a profile the default token is used")
	}
}

// Plan, "Profile token isolation": a profile with no token of its own never sends a request.
func TestProfileWithoutOwnTokenFailsBeforeAnyRequest(t *testing.T) {
	path := profileSetup(t, `"parse_mode":"html"`)
	for _, args := range [][]string{
		{"bot", "get"}, {"updates", "list"}, {"chats", "get"}, {"bot", "get", "--dry-run"}, {"updates", "list", "--dry-run"},
	} {
		h := newHarness(t)
		result := h.run(append(args, "--profile", "builds", "--config", path)...)
		if result.exit != 3 || result.errorCode() != "config" {
			t.Fatalf("%v: exit %d: %s", args, result.exit, result.raw)
		}
		if h.fake.total() != 0 {
			t.Errorf("%v: %d upstream requests, want 0", args, h.fake.total())
		}
		details := result.errorBody()["details"].(map[string]any)
		if details["profile"] != "builds" || details["would_use_source"] != "TELEGRAM_BOT_TOKEN" {
			t.Errorf("%v: details %v", args, details)
		}
		message := fmt.Sprint(result.errorBody()["message"])
		hint := fmt.Sprint(result.errorBody()["hint"])
		if message != "Profile 'builds' has no bot token of its own and would use the default bot's." ||
			!strings.Contains(hint, "profiles.builds.bot_token_file") || !strings.Contains(hint, "TELEGRAM_BOT_TOKEN_FILE_BUILDS") {
			t.Errorf("%v: message %q hint %q", args, message, hint)
		}
		if strings.Contains(result.raw, testToken) || strings.Contains(result.raw, testToken[:12]) {
			t.Errorf("token leaked: %s", result.raw)
		}
	}
}

func TestProfileScopedVariablesAreAccepted(t *testing.T) {
	path := profileSetup(t, `"parse_mode":"html"`)
	h := newHarness(t)
	h.env["TELEGRAM_BOT_TOKEN_BUILDS"] = profileToken
	if result := h.run("bot", "get", "--profile", "builds", "--config", path); result.exit != 0 || h.fake.all()[0].token != profileToken {
		t.Errorf("exit %d: %s", result.exit, result.raw)
	}
	h = newHarness(t)
	h.env["TELEGRAM_BOT_TOKEN_FILE_BUILDS"] = writeTokenFile(t, profileToken)
	if result := h.run("bot", "get", "--profile", "builds", "--config", path); result.exit != 0 || h.fake.all()[0].token != profileToken {
		t.Errorf("exit %d: %s", result.exit, result.raw)
	}
}

func TestDoctorReportsBotIdentity(t *testing.T) {
	h := newHarness(t)
	result := h.run("doctor")
	if result.exit != 0 {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
	checks := result.object()["checks"].([]any)
	names := []string{"config", "credentials", "network", "auth", "clock", "datasets", "writes"}
	if len(checks) != len(names) {
		t.Fatalf("checks %s", result.raw)
	}
	for index, name := range names {
		check := checks[index].(map[string]any)
		if check["name"] != name || check["status"] == "fail" {
			t.Errorf("check %d: %v", index, check)
		}
	}
	auth := checks[3].(map[string]any)["detail"].(string)
	if auth != "Authenticated as bot @test_bot (id 123456789)." {
		t.Errorf("auth detail %q", auth)
	}
	if credentials := checks[1].(map[string]any)["detail"].(string); !strings.Contains(credentials, "BOT_TOKEN from TELEGRAM_BOT_TOKEN") || strings.Contains(credentials, testToken) {
		t.Errorf("credentials detail %q", credentials)
	}
	if clock := checks[4].(map[string]any); clock["status"] != "pass" {
		t.Errorf("clock %v", clock)
	}
	if datasets := checks[5].(map[string]any); datasets["status"] != "pass" {
		t.Errorf("datasets %v", datasets)
	}
	if h.fake.count("getMe") != 1 || h.fake.total() != 1 {
		t.Errorf("doctor makes exactly one authenticated read: %d", h.fake.total())
	}
}

// Plan: doctor fail/skip/exit 3 for a refused profile token.
func TestDoctorFailsForAProfileWithoutItsOwnToken(t *testing.T) {
	h := newHarness(t)
	path := profileSetup(t, `"parse_mode":"html"`)
	result := h.run("doctor", "--profile", "builds", "--config", path)
	if result.exit != 3 || result.errorCode() != "config" {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
	checks := result.object()["checks"].([]any)
	status := map[string]string{}
	for _, check := range checks {
		item := check.(map[string]any)
		status[item["name"].(string)] = item["status"].(string)
	}
	want := map[string]string{"config": "pass", "credentials": "fail", "network": "skip", "auth": "skip", "clock": "skip", "datasets": "pass", "writes": "pass"}
	for name, expected := range want {
		if status[name] != expected {
			t.Errorf("check %s = %s, want %s", name, status[name], expected)
		}
	}
	if len(checks) != 7 {
		t.Errorf("every check is reported: %s", result.raw)
	}
	if h.fake.total() != 0 {
		t.Errorf("%d requests, want 0", h.fake.total())
	}
	if !strings.Contains(checks[1].(map[string]any)["detail"].(string), "TELEGRAM_BOT_TOKEN") {
		t.Errorf("detail should name the source: %v", checks[1])
	}
}

func TestDoctorFailsWhenTheTokenIsRejected(t *testing.T) {
	h := newHarness(t)
	h.fake.override["getMe"] = func(int) (int, string) { return 401, `{"ok":false,"error_code":401,"description":"Unauthorized"}` }
	result := h.run("doctor")
	if result.exit != 4 || result.errorCode() != "auth" || result.object() == nil {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
}

func TestListConfigShowsTheSkippedTokenSourceWithAHint(t *testing.T) {
	h := newHarness(t)
	path := profileSetup(t, `"parse_mode":"html"`)
	result := h.run("list-config", "--profile", "builds", "--config", path)
	if result.exit != 0 {
		t.Fatalf("list-config must work: exit %d: %s", result.exit, result.raw)
	}
	var token map[string]any
	names := map[string]bool{}
	for _, entry := range result.object()["settings"].([]any) {
		item := entry.(map[string]any)
		names[item["name"].(string)] = true
		if item["name"] == "BOT_TOKEN" {
			token = item
		}
	}
	for _, name := range []string{"BASE_URL", "DEFAULT_CHAT", "ALLOWED_CHATS", "PARSE_MODE", "POLL_INTERVAL", "DATASET_DIR", "DATASET_TTL", "LIMIT"} {
		if !names[name] {
			t.Errorf("list-config lacks %s", name)
		}
	}
	// The default-scope TELEGRAM_BOT_TOKEN is skipped for the profile, so nothing is set for it, and the
	// hint names the source that is being skipped.
	if token == nil || token["set"] != false || token["value"] != nil || token["source"] != "builtin" || token["origin"] != "builtin" {
		t.Fatalf("BOT_TOKEN row %v", token)
	}
	if hint := fmt.Sprint(token["hint"]); !strings.Contains(hint, "skips the default bot's token (TELEGRAM_BOT_TOKEN)") ||
		!strings.Contains(hint, "profiles.builds.bot_token_file") {
		t.Errorf("hint %q", hint)
	}
	if strings.Contains(result.raw, testToken) {
		t.Errorf("token leaked: %s", result.raw)
	}
	for _, entry := range h.run("list-config").object()["settings"].([]any) {
		if item := entry.(map[string]any); item["name"] == "BOT_TOKEN" && item["hint"] != nil {
			t.Errorf("no profile selected, no hint: %v", item)
		}
	}
	if h.fake.total() != 0 {
		t.Errorf("list-config contacted upstream")
	}
}

func TestUnknownProfileIsConfigWithListProfilesHint(t *testing.T) {
	h := newHarness(t)
	result := h.run("bot", "get", "--profile", "nope")
	if result.exit != 3 || result.errorCode() != "config" || !strings.Contains(fmt.Sprint(result.errorBody()["hint"]), "list-profiles") || h.fake.total() != 0 {
		t.Errorf("exit %d: %s", result.exit, result.raw)
	}
}

func TestListProfilesShowsDescriptions(t *testing.T) {
	h := newHarness(t)
	path := profileSetup(t, `"bot_token_file":"/x"`)
	h.env["TELEGRAM_POLL_INTERVAL_SCRATCH"] = "5s"
	listed := h.run("list-profiles", "--config", path)
	want := `"data":[` +
		`{"name":"default","active":true,"declared_in":[],"description":"PR-assistant bot."},` +
		`{"name":"builds","active":false,"declared_in":["config_file"],"description":"CI bot."},` +
		`{"name":"scratch","active":false,"declared_in":["environment"],"description":null}],`
	if listed.exit != 0 || !strings.Contains(listed.raw, want) || !strings.Contains(listed.raw, `"contract_version":"1.4.2"`) {
		t.Errorf("exit %d\ngot  %s\nwant %s", listed.exit, listed.raw, want)
	}
	if strings.Contains(listed.raw, "111111111") || strings.Contains(listed.raw, testToken) || h.fake.total() != 0 {
		t.Errorf("list-profiles carries names and descriptions only: %s", listed.raw)
	}
	human := h.run("list-profiles", "--config", path, "--human")
	if !strings.Contains(human.raw, "CI bot.") {
		t.Errorf("--human shows descriptions: %s", human.raw)
	}
}

func TestNoDatasetSurface(t *testing.T) {
	h := newHarness(t)
	if strings.Contains(h.run("tools").raw, "dataset") || strings.Contains(h.run("describe").raw, `"collectable":true`) {
		t.Error("this tool has no dataset commands and nothing is collectable")
	}
	for _, args := range [][]string{{"updates", "list", "--all"}, {"updates", "list", "--offset", "3"}, {"dataset", "list"}} {
		if result := h.run(args...); result.exit != 2 {
			t.Errorf("%v: exit %d, want usage: %s", args, result.exit, result.raw)
		}
	}
	// The DATASET_* settings stay: the contract gives every tool them, and doctor checks the directory.
	h.env["TELEGRAM_DATASET_DIR"] = filepath.Join(h.home, "datasets")
	if result := h.run("doctor"); result.exit != 0 || !strings.Contains(result.raw, "datasets") {
		t.Errorf("doctor: %s", result.raw)
	}
	if info, err := os.Stat(filepath.Join(h.home, "datasets")); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("the datasets check creates the directory owner-only: %v %v", info, err)
	}
	h.env["TELEGRAM_DATASET_DIR"] = filepath.Join(h.home, "config.json", "nope")
	if err := os.WriteFile(filepath.Join(h.home, "config.json"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if result := h.run("doctor"); result.exit != 3 {
		t.Errorf("an unusable dataset directory is config: exit %d: %s", result.exit, result.raw)
	}
}

func TestDescribeDeclaresChatUnreachable(t *testing.T) {
	h := newHarness(t)
	data := h.run("describe").object()
	codes, _ := data["exit_codes"].([]any)
	if len(codes) != 1 {
		t.Fatalf("exit_codes %v", codes)
	}
	code := codes[0].(map[string]any)
	if code["code"] != float64(32) || code["name"] != "chat_unreachable" || code["retriable"] != false || code["meaning"] == "" {
		t.Errorf("exit code %v", code)
	}
	if !strings.Contains(h.run("describe").raw, `"chat_unreachable"`) {
		t.Error("the envelope schema's code enum lists chat_unreachable")
	}
	if auth := data["auth"].(map[string]any)["credentials"].([]any); len(auth) != 1 ||
		auth[0].(map[string]any)["name"] != "BOT_TOKEN" || auth[0].(map[string]any)["env"] != "TELEGRAM_BOT_TOKEN" ||
		auth[0].(map[string]any)["profile_scoped"] != true {
		t.Errorf("auth %v", auth)
	}
	chats := h.run("describe", "chats.get").object()["commands"].([]any)[0].(map[string]any)
	if !strings.Contains(fmt.Sprint(chats["x-cli"].(map[string]any)["errors"]), "chat_unreachable") {
		t.Errorf("chats.get can fail with chat_unreachable: %v", chats["x-cli"])
	}
	updates := h.run("describe", "updates.list").object()["commands"].([]any)[0].(map[string]any)
	xcli := updates["x-cli"].(map[string]any)
	if xcli["time_window"] != true || xcli["collectable"] != false || xcli["paged"] != true || updates["sort"] != "update_id asc" {
		t.Errorf("updates.list x-cli %v", xcli)
	}
	limits := updates["limits"].(map[string]any)
	if limits["default_limit"] != float64(25) || limits["max_limit"] != float64(100) {
		t.Errorf("limits %v", limits)
	}
}

func TestVersionReportsContract141(t *testing.T) {
	h := newHarness(t)
	result := h.run("version")
	data := result.object()
	if data["contract_version"] != "1.4.2" || data["tool"] != "telegram" || result.meta()["contract_version"] != "1.4.2" {
		t.Errorf("version %s", result.raw)
	}
}

func TestDeterministicOutput(t *testing.T) {
	h := newHarness(t)
	h.fake.updates = []string{message(1, 111111111, time.Hour, "a"), message(2, 111111111, time.Hour, "b")}
	first := h.run("updates", "list", "--deterministic")
	second := h.run("updates", "list", "--deterministic")
	if first.raw != second.raw {
		t.Errorf("identical calls differ:\n%s\n%s", first.raw, second.raw)
	}
}

func TestLimitClampedWithWarning(t *testing.T) {
	h := newHarness(t)
	result := h.run("updates", "list", "--limit", "500")
	if result.exit != 0 || result.page()["limit"] != float64(100) || len(result.warningCodes()) != 1 || result.warningCodes()[0] != "limit_clamped" {
		t.Errorf("clamp: %s", result.raw)
	}
}

func TestByteCapResumesAtTheFirstDroppedUpdate(t *testing.T) {
	h := newHarness(t)
	for id := 1; id <= 6; id++ {
		h.fake.updates = append(h.fake.updates, message(id, 111111111, time.Hour, strings.Repeat("x", 200)))
	}
	capped := h.run("updates", "list", "--max-bytes", "1500")
	page := capped.page()
	if page["truncated"] != true || page["truncated_reason"] != "max_bytes" || page["has_more"] != true {
		t.Fatalf("page %v", page)
	}
	rest := h.run("updates", "list", "--cursor", page["next_cursor"].(string), "--max-bytes", "100000")
	if got := len(capped.data()) + len(rest.data()); got != 6 {
		t.Errorf("%d + %d records, want 6 in total", len(capped.data()), len(rest.data()))
	}
	first := rest.data()[0].(map[string]any)["update_id"].(float64)
	if first != float64(len(capped.data())+1) {
		t.Errorf("the cursor should resume at update %d, got %v", len(capped.data())+1, first)
	}
}
