package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guyravid/ai/cli/tools/trello/internal/commands"
	"github.com/guyravid/ai/cli/tools/trello/internal/registry"
)

const (
	testKey   = "0123456789abcdef0123456789abcdef"
	testToken = "ATTA0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab"
)

var fixedNow = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

// fakeTrello is a request-counting stand-in for api.trello.com.
type fakeTrello struct {
	mu       sync.Mutex
	requests map[string]int
	cards    int
	actions  int
	pageCap  int
	plant    string // a credential echoed into every card description
	lastBody string
	lastAuth string
}

func (f *fakeTrello) count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[key]
}

func (f *fakeTrello) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	sum := 0
	for _, n := range f.requests {
		sum += n
	}
	return sum
}

func (f *fakeTrello) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests[r.Method+" "+r.URL.Path]++
	f.lastAuth = r.Header.Get("Authorization")
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == "GET" && r.URL.Path == "/1/members/me/boards":
		fmt.Fprint(w, `[{"id":"b2","name":"Beta","closed":false,"desc":""},{"id":"b1","name":"Alpha","closed":false}]`)
	case r.Method == "GET" && r.URL.Path == "/1/boards/B/cards":
		var items []string
		for i := 0; i < f.cards; i++ {
			items = append(items, fmt.Sprintf(`{"id":"c%04d","name":"Card %d","idList":"L%d","desc":"%s %s","dateLastActivity":"2026-09-%02dT10:00:00.000Z","due":null}`,
				i, i, i%3, strings.Repeat("d", 300), f.plant, 1+i%28))
		}
		fmt.Fprint(w, "["+strings.Join(items, ",")+"]")
	case r.Method == "GET" && r.URL.Path == "/1/boards/B/actions":
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		limit = min(limit, f.pageCap)
		start := 0
		if before := r.URL.Query().Get("before"); strings.HasPrefix(before, "a") {
			index, _ := strconv.Atoi(strings.TrimPrefix(before, "a"))
			start = index + 1
		}
		var items []string
		for i := start; i < f.actions && len(items) < limit; i++ {
			date := fixedNow.Add(-time.Duration(i+1) * time.Minute).Format("2006-01-02T15:04:05.000Z")
			items = append(items, fmt.Sprintf(`{"id":"a%04d","type":"commentCard","date":"%s","idMemberCreator":"m1","data":{"text":"t%d"}}`, i, date, i))
		}
		fmt.Fprint(w, "["+strings.Join(items, ",")+"]")
	case r.Method == "GET" && r.URL.Path == "/1/cards/missing":
		w.WriteHeader(404)
		fmt.Fprint(w, "The requested resource was not found.")
	case r.Method == "POST" && r.URL.Path == "/1/cards":
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(r.Body)
		f.mu.Lock()
		f.lastBody = body.String()
		f.mu.Unlock()
		fmt.Fprint(w, `{"id":"new1","name":"Fix paging","idList":"L1"}`)
	default:
		w.WriteHeader(404)
	}
}

type harness struct {
	t        *testing.T
	fake     *fakeTrello
	app      *App
	env      map[string]string
	cacheDir string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	fake := &fakeTrello{requests: map[string]int{}, cards: 214, actions: 100, pageCap: 1000}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	home := t.TempDir()
	cache := t.TempDir()
	env := map[string]string{
		"HOME":             home,
		"XDG_CONFIG_HOME":  filepath.Join(home, "config"),
		"XDG_CACHE_HOME":   cache,
		"TRELLO_BASE_URL":  server.URL + "/1",
		"TRELLO_API_KEY":   testKey,
		"TRELLO_API_TOKEN": testToken,
	}
	all := append(registry.Builtins(), commands.Reads()...)
	all = append(all, commands.Writes()...)
	reg := registry.New(all)
	if err := reg.Validate(); err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, fake: fake, env: env, cacheDir: cache}
	h.app = &App{
		Build: Build{Tool: "trello", Prefix: "TRELLO", Version: "test", WritesEnabled: commands.WritesEnabled,
			GOOS: "linux", GOARCH: "arm64"},
		Registry:      reg,
		Env:           func(name string) (string, bool) { value, ok := h.env[name]; return value, ok },
		Now:           func() time.Time { return fixedNow },
		MutatingNames: commands.MutatingNames,
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

func (r result) page() map[string]any {
	meta, _ := r.doc["meta"].(map[string]any)
	page, _ := meta["page"].(map[string]any)
	return page
}

func (r result) data() []any {
	items, _ := r.doc["data"].([]any)
	return items
}

func (r result) errorCode() string {
	body, _ := r.doc["error"].(map[string]any)
	code, _ := body["code"].(string)
	return code
}

func TestLimitFillsAcrossPages(t *testing.T) {
	h := newHarness(t)
	h.fake.pageCap = 20
	previous := commands.ActionsPageSize
	commands.ActionsPageSize = 20
	t.Cleanup(func() { commands.ActionsPageSize = previous })

	out := h.run("actions", "list", "--board", "B", "--limit", "60")
	if out.exit != 0 || len(out.data()) != 60 {
		t.Fatalf("exit %d, %d records: %s", out.exit, len(out.data()), out.raw)
	}
	if got := h.fake.count("GET /1/boards/B/actions"); got != 3 {
		t.Errorf("upstream requests = %d, want 3", got)
	}
	page := out.page()
	if page["has_more"] != true || page["next_cursor"] == nil || page["total"] != nil || page["total_is_exact"] != false {
		t.Errorf("page = %v", page)
	}

	// The cursor alone continues where the first call stopped.
	next := h.run("actions", "list", "--cursor", page["next_cursor"].(string))
	first := next.data()[0].(map[string]any)["id"]
	if next.exit != 0 || first != "a0060" {
		t.Fatalf("resumed at %v (exit %d): %s", first, next.exit, next.raw)
	}
	meta := next.doc["meta"].(map[string]any)
	if meta["window"].(map[string]any)["source"] != "cursor" {
		t.Errorf("window source = %v", meta["window"])
	}
}

func TestDefaultWindowIsReported(t *testing.T) {
	h := newHarness(t)
	out := h.run("actions", "list", "--board", "B")
	window := out.doc["meta"].(map[string]any)["window"].(map[string]any)
	if window["source"] != "default" || window["since"] != "2026-09-29T12:00:00Z" || window["until"] != "2026-09-30T12:00:00Z" {
		t.Errorf("window = %v", window)
	}
	for _, bad := range []string{"2h", "+2h", "2026-09-29T14:00:00"} {
		if out := h.run("actions", "list", "--board", "B", "--since", bad); out.exit != 2 {
			t.Errorf("--since %s gave exit %d", bad, out.exit)
		}
	}
}

func TestByteCapResumesAtFirstDroppedRecord(t *testing.T) {
	h := newHarness(t)
	out := h.run("cards", "list", "--board", "B", "--limit", "50", "--fields", "id,desc", "--max-bytes", "4000")
	page := out.page()
	if out.exit != 0 || page["truncated"] != true || page["truncated_reason"] != "max_bytes" {
		t.Fatalf("exit %d page %v", out.exit, page)
	}
	if len(out.raw) > 4000 {
		t.Errorf("output %d bytes", len(out.raw))
	}
	kept := len(out.data())
	if int(page["dropped"].(float64)) != 50-kept {
		t.Errorf("dropped = %v, kept %d", page["dropped"], kept)
	}
	lastID := out.data()[kept-1].(map[string]any)["id"].(string)
	next := h.run("cards", "list", "--cursor", page["next_cursor"].(string), "--max-bytes", "4000")
	all := h.run("cards", "list", "--board", "B", "--limit", "100", "--fields", "id", "--max-bytes", "100000")
	var ids []string
	for _, item := range all.data() {
		ids = append(ids, item.(map[string]any)["id"].(string))
	}
	for index, id := range ids {
		if id == lastID {
			want := ids[index+1]
			if got := next.data()[0].(map[string]any)["id"]; got != want {
				t.Fatalf("resumed at %v, want %s", got, want)
			}
			return
		}
	}
	t.Fatal("last kept id not found")
}

func TestCollectThenReadFromDisk(t *testing.T) {
	h := newHarness(t)
	h.fake.pageCap = 20
	previous := commands.ActionsPageSize
	commands.ActionsPageSize = 20
	t.Cleanup(func() { commands.ActionsPageSize = previous })

	paged := 0
	cursor := ""
	for {
		args := []string{"actions", "list", "--board", "B", "--since", "-1d", "--limit", "10"}
		if cursor != "" {
			args = []string{"actions", "list", "--cursor", cursor}
		}
		out := h.run(args...)
		paged += len(out.data())
		next, _ := out.page()["next_cursor"].(string)
		if next == "" {
			break
		}
		cursor = next
	}
	pagedRequests := h.fake.total()

	h.fake.requests = map[string]int{}
	collected := h.run("actions", "list", "--board", "B", "--since", "-1d", "--all", "--limit", "10")
	collectRequests := h.fake.total()
	if collected.exit != 0 {
		t.Fatalf("collect failed: %s", collected.raw)
	}
	if collectRequests >= pagedRequests {
		t.Errorf("--all used %d requests, paging used %d", collectRequests, pagedRequests)
	}
	dataset := collected.doc["meta"].(map[string]any)["dataset"].(map[string]any)
	path := dataset["path"].(string)
	if dataset["record_count"].(float64) != float64(paged) || dataset["complete"] != true {
		t.Errorf("dataset = %v, paged %d", dataset, paged)
	}
	content, err := os.ReadFile(path)
	if err != nil || strings.Count(string(content), "\n") != paged {
		t.Fatalf("dataset file: %v, %d lines", err, strings.Count(string(content), "\n"))
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("dataset mode %o", info.Mode().Perm())
	}

	h.fake.requests = map[string]int{}
	read := 0
	cursor = collected.page()["next_cursor"].(string)
	read += len(collected.data())
	for cursor != "" {
		out := h.run("dataset", "read", "--cursor", cursor)
		read += len(out.data())
		cursor, _ = out.page()["next_cursor"].(string)
	}
	if read != paged || h.fake.total() != 0 {
		t.Errorf("read %d of %d, with %d upstream requests", read, paged, h.fake.total())
	}
}

func TestDatasetExpiresAfterTTL(t *testing.T) {
	h := newHarness(t)
	collected := h.run("boards", "list", "--all")
	id := collected.doc["meta"].(map[string]any)["dataset"].(map[string]any)["id"].(string)
	if out := h.run("dataset", "read", id); out.exit != 0 {
		t.Fatalf("read failed: %s", out.raw)
	}
	h.app.Now = func() time.Time { return fixedNow.Add(2 * time.Hour) }
	out := h.run("dataset", "read", id)
	if out.exit != 14 || out.errorCode() != "cache_miss" || !strings.Contains(out.raw, "trello boards list") {
		t.Fatalf("exit %d: %s", out.exit, out.raw)
	}
}

func TestWritesSendNothingWithoutConfirm(t *testing.T) {
	if !commands.WritesEnabled {
		t.Skip("read-only build")
	}
	h := newHarness(t)
	refused := h.run("cards", "create", "--list-id", "L1", "--name", "Fix paging")
	dry := h.run("cards", "create", "--list-id", "L1", "--name", "Fix paging", "--dry-run", "--confirm")
	if refused.exit != 8 || refused.errorCode() != "refused" || dry.exit != 0 {
		t.Fatalf("refused %d, dry %d", refused.exit, dry.exit)
	}
	if h.fake.total() != 0 {
		t.Fatalf("%d requests sent without confirmation", h.fake.total())
	}
	preview := refused.doc["error"].(map[string]any)["details"].(map[string]any)["preview"].(map[string]any)
	if preview["method"] != "POST" || !strings.HasSuffix(preview["url"].(string), "/1/cards") ||
		preview["headers"].(map[string]any)["Authorization"] != "***REDACTED***" {
		t.Errorf("preview = %v", preview)
	}
	confirmed := h.run("cards", "create", "--list-id", "L1", "--name", "Fix paging", "--confirm")
	if confirmed.exit != 0 || h.fake.count("POST /1/cards") != 1 || !strings.Contains(h.fake.lastBody, `"idList":"L1"`) {
		t.Fatalf("confirmed exit %d, body %s", confirmed.exit, h.fake.lastBody)
	}
	if !strings.Contains(h.fake.lastAuth, `oauth_consumer_key="`+testKey+`"`) {
		t.Error("credentials were not sent in the Authorization header")
	}
}

func TestPlantedCredentialNeverEscapes(t *testing.T) {
	h := newHarness(t)
	h.fake.plant = testToken
	h.fake.cards = 5
	outputs := []result{
		h.run("cards", "list", "--board", "B", "--fields", "id,desc", "--max-string", "65536", "--verbose"),
		h.run("cards", "list", "--board", "B", "--fields", "id,desc", "--all", "--human"),
	}
	for _, out := range outputs {
		for _, text := range []string{out.raw, out.stderr} {
			if strings.Contains(text, testToken) || strings.Contains(text, testToken[:12]) || strings.Contains(text, testKey) {
				t.Fatalf("credential leaked:\n%s", text)
			}
		}
	}
	entries, _ := filepath.Glob(filepath.Join(h.cacheDir, "agentcli", "trello", "datasets", "*.jsonl"))
	if len(entries) == 0 {
		t.Fatal("no dataset written")
	}
	for _, entry := range entries {
		content, _ := os.ReadFile(entry)
		if bytes.Contains(content, []byte(testToken[:12])) {
			t.Fatal("credential leaked into a dataset")
		}
	}
}

func TestErrorsMapToContractCodes(t *testing.T) {
	h := newHarness(t)
	if out := h.run("cards", "get", "missing"); out.exit != 5 || out.errorCode() != "not_found" {
		t.Errorf("not found: exit %d %s", out.exit, out.raw)
	}
	delete(h.env, "TRELLO_API_TOKEN")
	if out := h.run("boards", "list"); out.exit != 4 || out.errorCode() != "auth" || h.fake.total() != 1 {
		t.Errorf("missing token: exit %d, %d requests: %s", out.exit, h.fake.total(), out.raw)
	}
	if out := h.run("cards", "list", "--board", "B", "--fields", "id,nmae"); out.exit != 2 {
		t.Errorf("bad field: exit %d", out.exit)
	}
	if out := h.run("cards", "get", "missing", "--human"); out.exit != 4 || !strings.Contains(out.raw, "ERROR auth") {
		t.Errorf("--human error: exit %d %q", out.exit, out.raw)
	}
}

func TestEmptyListIsSuccess(t *testing.T) {
	h := newHarness(t)
	h.fake.cards = 0
	out := h.run("cards", "list", "--board", "B")
	if out.exit != 0 || out.raw == "" || len(out.data()) != 0 || out.page()["count"].(float64) != 0 {
		t.Fatalf("exit %d: %s", out.exit, out.raw)
	}
	if !strings.Contains(out.raw, `"data":[]`) {
		t.Errorf("data is not an empty array: %s", out.raw)
	}
}

func TestLimitClampedWithWarning(t *testing.T) {
	h := newHarness(t)
	out := h.run("boards", "list", "--limit", "5000")
	meta := out.doc["meta"].(map[string]any)
	if out.exit != 0 || out.page()["limit"].(float64) != 200 || !strings.Contains(out.raw, "limit_clamped") {
		t.Fatalf("exit %d meta %v", out.exit, meta)
	}
}

func TestDeterministicOutput(t *testing.T) {
	h := newHarness(t)
	first := h.run("cards", "list", "--board", "B", "--all", "--deterministic")
	second := h.run("cards", "list", "--board", "B", "--all", "--deterministic")
	if first.raw != second.raw {
		t.Fatalf("outputs differ:\n%s\n%s", first.raw, second.raw)
	}
	path := first.doc["meta"].(map[string]any)["dataset"].(map[string]any)["path"].(string)
	if filepath.IsAbs(path) {
		t.Errorf("deterministic path is absolute: %s", path)
	}
}
