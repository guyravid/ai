package app

import (
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/guyravid/ai/cli/tools/telegram/internal/commands"
)

// writesHarness is a full build whose profile may address chat 111111111.
func writesHarness(t *testing.T) *harness {
	t.Helper()
	if !commands.WritesEnabled {
		t.Skip("read-only build: there are no writes to test")
	}
	h := newHarness(t)
	h.env["TELEGRAM_DEFAULT_CHAT"] = "111111111"
	return h
}

func (r result) preview() map[string]any {
	if preview, ok := r.object()["preview"].(map[string]any); ok {
		return preview
	}
	preview, _ := r.errorBody()["details"].(map[string]any)["preview"].(map[string]any)
	return preview
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWritesAreDeclaredMutatingWithTheRightDestructiveHint(t *testing.T) {
	h := writesHarness(t)
	detail := h.run("tools", "--detail").raw
	described := h.run("describe").raw
	for _, name := range []string{"messages.send", "messages.send-document", "messages.send-photo", "updates.ack"} {
		if !strings.Contains(detail, fmt.Sprintf(`"name":"%s"`, name)) {
			t.Fatalf("tools lacks %s", name)
		}
		entry := h.run("describe", name)
		annotations := entry.doc["data"].(map[string]any)["commands"].([]any)[0].(map[string]any)["annotations"].(map[string]any)
		wantDestructive := name == "updates.ack"
		if annotations["readOnlyHint"] != false || annotations["destructiveHint"] != wantDestructive {
			t.Errorf("%s annotations %v, want readOnlyHint false and destructiveHint %v", name, annotations, wantDestructive)
		}
	}
	if !strings.Contains(described, `"writes_enabled":true`) {
		t.Error("describe reports writes_enabled true")
	}
	for _, command := range h.app.Registry.Commands() {
		if mutates := command.IsWrite(); mutates != strings.Contains(strings.Join(commands.MutatingNames, ","), command.Name) {
			t.Errorf("%s: mutates=%v disagrees with MutatingNames", command.Name, mutates)
		}
	}
}

func TestWriteWithoutConfirmIsRefusedWithAPreviewAndNoRequest(t *testing.T) {
	h := writesHarness(t)
	file := writeTemp(t, "report.txt", "FILE-CONTENT-NEVER-SHOWN")
	for _, args := range [][]string{
		{"messages", "send", "--text", "hello"},
		{"messages", "send-document", "--file", file},
		{"messages", "send-photo", "--file", file},
		{"updates", "ack", "--through", "5"},
	} {
		result := h.run(args...)
		if result.exit != 8 || result.errorCode() != "refused" || result.detail("reason") != "confirmation_required" {
			t.Errorf("%v: exit %d: %s", args, result.exit, result.raw)
			continue
		}
		preview := result.preview()
		if preview == nil || preview["method"] != "POST" || !strings.HasSuffix(fmt.Sprint(preview["url"]), "/bot<redacted>/"+map[string]string{
			"send": "sendMessage", "send-document": "sendDocument", "send-photo": "sendPhoto", "ack": "getUpdates"}[args[1]]) {
			t.Errorf("%v: preview %v", args, preview)
		}
		if !strings.Contains(fmt.Sprint(result.errorBody()["hint"]), "--confirm") {
			t.Errorf("%v: the hint is the command with --confirm", args)
		}
		if strings.Contains(result.raw, "FILE-CONTENT-NEVER-SHOWN") {
			t.Errorf("%v: a preview never shows file contents", args)
		}
	}
	if h.fake.total() != 0 {
		t.Errorf("%d requests for refused writes", h.fake.total())
	}
}

func TestDryRunSendsNothingAndShowsThePreview(t *testing.T) {
	h := writesHarness(t)
	file := writeTemp(t, "report.txt", "FILE-CONTENT-NEVER-SHOWN")
	for _, args := range [][]string{
		{"messages", "send", "--text", "hello", "--dry-run"},
		{"messages", "send", "--text", "hello", "--confirm", "--dry-run"},
		{"messages", "send-document", "--file", file, "--confirm", "--dry-run"},
		{"messages", "send-photo", "--file", file, "--dry-run"},
		{"updates", "ack", "--through", "5", "--confirm", "--dry-run"},
	} {
		result := h.run(args...)
		if result.exit != 0 || result.meta()["dry_run"] != true || result.preview() == nil {
			t.Errorf("%v: exit %d: %s", args, result.exit, result.raw)
		}
		if strings.Contains(result.raw, "FILE-CONTENT-NEVER-SHOWN") {
			t.Errorf("%v: a preview never shows file contents", args)
		}
	}
	if h.fake.total() != 0 {
		t.Errorf("%d requests during dry runs", h.fake.total())
	}
}

func TestSendPostsTheMessageAndReturnsItsId(t *testing.T) {
	h := writesHarness(t)
	result := h.run("messages", "send", "--text", "Deploy finished", "--confirm")
	if result.exit != 0 {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
	data := result.object()
	if data["message_id"] != float64(4001) || data["date"] != "2026-09-30T12:00:00Z" || data["text"] != "echo" {
		t.Errorf("data %v", data)
	}
	if chat, _ := data["chat"].(map[string]any); chat["id"] != float64(111111111) {
		t.Errorf("chat %v", data["chat"])
	}
	requests := h.fake.all()
	if len(requests) != 1 || requests[0].method != "sendMessage" || requests[0].token != testToken ||
		requests[0].body != `{"chat_id":111111111,"text":"Deploy finished"}` {
		t.Fatalf("requests %+v", requests)
	}
	if !strings.Contains(requests[0].header.Get("Content-Type"), "application/json") {
		t.Errorf("content type %q", requests[0].header.Get("Content-Type"))
	}
}

func TestParseModeComesFromTheSettingAndTheFlagOverridesIt(t *testing.T) {
	h := writesHarness(t)
	cases := []struct {
		setting string
		flag    []string
		want    string
	}{
		{"", nil, `{"chat_id":111111111,"text":"x"}`},
		{"html", nil, `{"chat_id":111111111,"parse_mode":"HTML","text":"x"}`},
		{"html", []string{"--parse-mode", "none"}, `{"chat_id":111111111,"text":"x"}`},
		{"none", []string{"--parse-mode", "markdownv2"}, `{"chat_id":111111111,"parse_mode":"MarkdownV2","text":"x"}`},
	}
	for _, tc := range cases {
		if tc.setting == "" {
			delete(h.env, "TELEGRAM_PARSE_MODE")
		} else {
			h.env["TELEGRAM_PARSE_MODE"] = tc.setting
		}
		before := h.fake.total()
		args := append([]string{"messages", "send", "--text", "x", "--confirm"}, tc.flag...)
		if result := h.run(args...); result.exit != 0 {
			t.Fatalf("%v: %s", args, result.raw)
		}
		if got := h.fake.all()[before].body; got != tc.want {
			t.Errorf("setting %q flag %v: body %s, want %s", tc.setting, tc.flag, got, tc.want)
		}
	}
	if result := h.run("messages", "send", "--text", "x", "--parse-mode", "rtf", "--confirm"); result.exit != 2 {
		t.Errorf("an unknown parse mode is usage: %s", result.raw)
	}
}

func TestSilentAndReplyToShapeTheBody(t *testing.T) {
	h := writesHarness(t)
	h.run("messages", "send", "--text", "x", "--silent", "--reply-to", "4021", "--confirm")
	want := `{"chat_id":111111111,"disable_notification":true,"reply_parameters":{"message_id":4021},"text":"x"}`
	if got := h.fake.all()[0].body; got != want {
		t.Errorf("body %s, want %s", got, want)
	}
}

func TestTextAndTextFileAreMutuallyExclusiveAndOneIsRequired(t *testing.T) {
	h := writesHarness(t)
	file := writeTemp(t, "message.txt", "from a file\nsecond line é")
	directory := t.TempDir()
	for name, args := range map[string][]string{
		"both":        {"messages", "send", "--text", "a", "--text-file", file, "--confirm"},
		"neither":     {"messages", "send", "--confirm"},
		"a directory": {"messages", "send", "--text-file", directory, "--confirm"},
		"a missing":   {"messages", "send", "--text-file", filepath.Join(directory, "nope"), "--confirm"},
	} {
		if result := h.run(args...); result.exit != 2 || result.errorCode() != "usage" {
			t.Errorf("%s: exit %d: %s", name, result.exit, result.raw)
		}
	}
	notUTF8 := writeTemp(t, "binary", "\xff\xfe\x00")
	if result := h.run("messages", "send", "--text-file", notUTF8, "--confirm"); result.exit != 2 {
		t.Errorf("text that is not UTF-8 is usage: %s", result.raw)
	}
	if h.fake.total() != 0 {
		t.Errorf("%d requests for usage errors", h.fake.total())
	}
	if result := h.run("messages", "send", "--text-file", file, "--confirm"); result.exit != 0 {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
	if got := h.fake.all()[0].body; got != `{"chat_id":111111111,"text":"from a file\nsecond line é"}` {
		t.Errorf("body %s", got)
	}
}

func TestSendToAChatOutsideTheAllowlistIsRefusedUnlessAllowAnyChat(t *testing.T) {
	h := writesHarness(t)
	result := h.run("messages", "send", "--text", "hi", "--chat", "999999999", "--confirm")
	if result.exit != 8 || result.detail("reason") != "chat_not_allowed" || h.fake.total() != 0 {
		t.Fatalf("exit %d after %d requests: %s", result.exit, h.fake.total(), result.raw)
	}
	if hint := fmt.Sprint(result.errorBody()["hint"]); !strings.Contains(hint, "--allow-any-chat") || !strings.Contains(hint, "--chat 999999999") {
		t.Errorf("hint %q", hint)
	}
	if result := h.run("messages", "send", "--text", "hi", "--chat", "999999999", "--allow-any-chat", "--confirm"); result.exit != 0 {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
	if got := h.fake.all()[0].body; got != `{"chat_id":999999999,"text":"hi"}` {
		t.Errorf("body %s", got)
	}
}

func TestPlantedTokenNeverEscapesAWrite(t *testing.T) {
	h := writesHarness(t)
	file := writeTemp(t, "report.txt", "content")
	var all []string
	for _, args := range [][]string{
		{"messages", "send", "--text", "hello"},
		{"messages", "send", "--text", "hello", "--dry-run"},
		{"messages", "send", "--text", "hello", "--confirm", "--verbose"},
		{"messages", "send-document", "--file", file, "--confirm", "--verbose"},
		{"updates", "ack", "--through", "3", "--confirm", "--verbose"},
	} {
		result := h.run(args...)
		all = append(all, result.raw, result.stderr)
	}
	// A dial error embeds the URL, and so the token; it must be masked on a write too.
	h.env["TELEGRAM_BASE_URL"] = "http://127.0.0.1:1"
	dial := h.run("messages", "send", "--text", "hello", "--confirm", "--verbose")
	all = append(all, dial.raw, dial.stderr)
	if dial.errorCode() != "network" {
		t.Errorf("dial error: %s", dial.raw)
	}
	joined := strings.Join(all, "\n")
	for _, secret := range []string{testToken, testToken[:12], "bot" + testToken} {
		if strings.Contains(joined, secret) {
			t.Errorf("the token %q... escaped into output", secret[:12])
		}
	}
	if !strings.Contains(joined, "bot<redacted>") {
		t.Error("previews show bot<redacted>")
	}
}

func TestRateLimitedWriteIsNotRetried(t *testing.T) {
	h := writesHarness(t)
	h.fake.override["sendMessage"] = func(int) (int, string) {
		return 429, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 3","parameters":{"retry_after":3}}`
	}
	result := h.run("messages", "send", "--text", "hi", "--confirm")
	if result.exit != 9 || result.errorCode() != "rate_limited" || result.errorBody()["retry_after_ms"] != float64(3000) {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
	if h.fake.count("sendMessage") != 1 || len(h.sleeps) != 0 {
		t.Errorf("%d requests and waits %v: a write is never retried, not even on 429", h.fake.count("sendMessage"), h.sleeps)
	}
}

func TestServerErrorOnAWriteIsNotRetried(t *testing.T) {
	h := writesHarness(t)
	h.fake.override["sendMessage"] = func(int) (int, string) { return 502, `{"ok":false,"error_code":502,"description":"Bad Gateway"}` }
	result := h.run("messages", "send", "--text", "hi", "--confirm")
	if result.exit != 12 || h.fake.count("sendMessage") != 1 {
		t.Errorf("exit %d after %d requests: %s", result.exit, h.fake.count("sendMessage"), result.raw)
	}
}

func TestTimedOutWriteHasUnknownState(t *testing.T) {
	h := writesHarness(t)
	h.fake.override["sendMessage"] = func(int) (int, string) {
		time.Sleep(600 * time.Millisecond)
		return 200, `{"ok":true,"result":{"message_id":1,"chat":{"id":111111111,"type":"private"},"date":1}}`
	}
	result := h.run("messages", "send", "--text", "hi", "--confirm", "--timeout", "100ms")
	if result.exit != 10 || result.errorCode() != "timeout" || result.detail("write_state") != "unknown" {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
	if h.fake.count("sendMessage") != 1 {
		t.Errorf("%d requests: a timed-out write is never retried", h.fake.count("sendMessage"))
	}
	if hint := fmt.Sprint(result.errorBody()["hint"]); !strings.Contains(hint, "Check") {
		t.Errorf("hint %q says to check before trying again", hint)
	}
}

func TestSendChatUnreachableAndNotFoundMapToTheirCodes(t *testing.T) {
	h := writesHarness(t)
	h.fake.override["sendMessage"] = func(int) (int, string) {
		return 403, `{"ok":false,"error_code":403,"description":"Forbidden: bot can't initiate conversation with a user"}`
	}
	if result := h.run("messages", "send", "--text", "hi", "--confirm"); result.exit != 32 || result.errorCode() != "chat_unreachable" {
		t.Errorf("exit %d: %s", result.exit, result.raw)
	}
	h.fake.override["sendMessage"] = func(int) (int, string) {
		return 400, `{"ok":false,"error_code":400,"description":"Bad Request: message is too long"}`
	}
	result := h.run("messages", "send", "--text", "hi", "--confirm")
	if result.exit != 6 || !strings.Contains(fmt.Sprint(result.errorBody()["hint"]), "4096") {
		t.Errorf("validation hint states the limit: %s", result.raw)
	}
}

// parseMultipart returns the form fields and the file part of an upload request.
func parseMultipart(t *testing.T, request seen) (fields map[string]string, fileField, fileName, fileContent string) {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(request.header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("content type %q", request.header.Get("Content-Type"))
	}
	reader := multipart.NewReader(strings.NewReader(request.body), params["boundary"])
	fields = map[string]string{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(part)
		if part.FileName() != "" {
			fileField, fileName, fileContent = part.FormName(), part.FileName(), string(content)
			continue
		}
		fields[part.FormName()] = string(content)
	}
}

func TestUploadSendsMultipartAndThePreviewShowsPathBytesNameOnly(t *testing.T) {
	h := writesHarness(t)
	file := writeTemp(t, "report.txt", "SECRET-FILE-BODY")
	resolved, _ := filepath.EvalSymlinks(file)

	preview := h.run("messages", "send-document", "--file", file, "--caption", "Weekly", "--silent", "--reply-to", "7", "--dry-run")
	body, _ := preview.preview()["body"].(map[string]any)
	document, _ := body["document"].(map[string]any)
	if document["path"] != resolved || document["name"] != "report.txt" || document["bytes"] != float64(len("SECRET-FILE-BODY")) || len(document) != 3 {
		t.Errorf("document preview %v, want path, bytes and name only", document)
	}
	if body["chat_id"] != "111111111" || body["caption"] != "Weekly" || body["disable_notification"] != "true" ||
		body["reply_parameters"] != `{"message_id":7}` {
		t.Errorf("body preview %v", body)
	}
	if strings.Contains(preview.raw, "SECRET-FILE-BODY") {
		t.Error("a preview never shows file contents")
	}

	for method, field := range map[string]string{"send-document": "document", "send-photo": "photo"} {
		before := h.fake.total()
		result := h.run("messages", method, "--file", file, "--caption", "Weekly", "--confirm")
		if result.exit != 0 || result.object()["message_id"] == nil {
			t.Fatalf("%s: exit %d: %s", method, result.exit, result.raw)
		}
		request := h.fake.all()[before]
		wantMethod := map[string]string{"send-document": "sendDocument", "send-photo": "sendPhoto"}[method]
		if request.method != wantMethod {
			t.Errorf("%s sent %s", method, request.method)
		}
		fields, fileField, fileName, content := parseMultipart(t, request)
		if fileField != field || fileName != "report.txt" || content != "SECRET-FILE-BODY" {
			t.Errorf("%s: file part %q %q %q", method, fileField, fileName, content)
		}
		if fields["chat_id"] != "111111111" || fields["caption"] != "Weekly" {
			t.Errorf("%s: fields %v", method, fields)
		}
	}
	if strings.Contains(h.run("messages", "send-document", "--file", file).raw, "SECRET-FILE-BODY") {
		t.Error("a refusal never shows file contents")
	}
}

func TestUploadResolvesAndReportsASymlink(t *testing.T) {
	h := writesHarness(t)
	target := writeTemp(t, "real-name.txt", "x")
	link := filepath.Join(t.TempDir(), "innocent-link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	resolved, _ := filepath.EvalSymlinks(target)
	result := h.run("messages", "send-document", "--file", link, "--dry-run")
	body, _ := result.preview()["body"].(map[string]any)
	document, _ := body["document"].(map[string]any)
	if document["path"] != resolved || document["name"] != "real-name.txt" {
		t.Errorf("the preview reports the resolved file, not the link: %v", document)
	}
}

func TestUploadRefusesAnythingButARegularFile(t *testing.T) {
	h := writesHarness(t)
	directory := t.TempDir()
	for name, path := range map[string]string{"a directory": directory, "a missing file": filepath.Join(directory, "nope")} {
		for _, extra := range [][]string{{"--confirm"}, {"--dry-run"}, {}} {
			args := append([]string{"messages", "send-document", "--file", path}, extra...)
			if result := h.run(args...); result.exit != 2 || result.errorCode() != "usage" {
				t.Errorf("%s %v: exit %d: %s", name, extra, result.exit, result.raw)
			}
		}
	}
	if h.fake.total() != 0 {
		t.Errorf("%d requests", h.fake.total())
	}
}

func TestAckSendsTheOffsetAndReportsHasMore(t *testing.T) {
	h := writesHarness(t)
	result := h.run("updates", "ack", "--through", "812345", "--confirm")
	if result.exit != 0 || result.object()["acked_through"] != float64(812345) || result.object()["has_more"] != false {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
	if got := h.fake.all()[0].body; got != `{"limit":1,"offset":812346,"timeout":0}` {
		t.Errorf("body %s, want offset through+1, limit 1, timeout 0", got)
	}
	h.fake.updates = []string{message(812347, 111111111, time.Hour, "next")}
	if result := h.run("updates", "ack", "--through", "812345", "--confirm"); result.object()["has_more"] != true {
		t.Errorf("an update still showing after the ack means has_more: %s", result.raw)
	}
	preview := h.run("updates", "ack", "--through", "9")
	body, _ := preview.preview()["body"].(map[string]any)
	if body["offset"] != float64(10) {
		t.Errorf("the refusal preview shows the offset it would send: %v", body)
	}
	if h.run("updates", "ack", "--through", "-1", "--confirm").exit != 2 {
		t.Error("a negative update_id is usage")
	}
	if h.run("updates", "ack", "--confirm").exit != 2 {
		t.Error("--through is required")
	}
}

// Plan, trap 2: only updates.ack ever sends an offset.
func TestOnlyAckEverSendsAnOffset(t *testing.T) {
	h := writesHarness(t)
	h.env["TELEGRAM_ALLOWED_CHATS"] = "111111111"
	file := writeTemp(t, "f.txt", "x")
	h.fake.updates = []string{message(1, 111111111, time.Hour, "old")}
	for _, args := range [][]string{
		{"bot", "get"}, {"chats", "get"}, {"doctor"}, {"updates", "list"}, {"updates", "list", "--allow-any-chat"},
		{"messages", "send", "--text", "x", "--confirm"}, {"messages", "send", "--text", "x"},
		{"messages", "send-document", "--file", file, "--confirm"}, {"messages", "send-photo", "--file", file, "--confirm"},
		{"messages", "send", "--text", "x", "--dry-run"},
		{"updates", "wait", "--max-wait", "1s", "--after-message", "1"}, {"updates", "wait", "--dry-run"},
	} {
		h.run(args...)
	}
	for _, request := range h.fake.all() {
		if strings.Contains(request.body, "offset") || strings.Contains(request.query, "offset") ||
			strings.Contains(request.body, "allowed_updates") || strings.Contains(request.query, "allowed_updates") {
			t.Errorf("%s sent an offset or allowed_updates: %q %q", request.method, request.body, request.query)
		}
	}
	before := h.fake.total()
	h.run("updates", "ack", "--through", "1", "--confirm")
	acks := h.fake.all()[before:]
	if len(acks) != 1 || !strings.Contains(acks[0].body, `"offset":2`) {
		t.Errorf("the ack is the one request with an offset: %+v", acks)
	}
}

func TestSendResultsCarryTheUploadedFileAndTheReplyLink(t *testing.T) {
	h := writesHarness(t)
	file := writeTemp(t, "report.txt", "body")
	const header = `{"ok":true,"result":{"message_id":40,"date":1790000000,"chat":{"id":111111111,"type":"private"},`
	h.fake.override["sendDocument"] = func(int) (int, string) {
		return 200, header + `"document":{"file_id":"D1","file_unique_id":"U1","file_name":"report.txt","mime_type":"text/plain","file_size":4}}}`
	}
	h.fake.override["sendPhoto"] = func(int) (int, string) {
		return 200, header + `"photo":[{"file_id":"s","width":90,"height":60},{"file_id":"big","width":1280,"height":853},{"file_id":"m","width":320,"height":213}]}}`
	}
	h.fake.override["sendMessage"] = func(int) (int, string) {
		return 200, header + `"text":"x","reply_to_message":{"message_id":33,"text":"orig"}}}`
	}

	document := h.run("messages", "send-document", "--file", file, "--confirm").object()
	want := map[string]any{"file_id": "D1", "file_unique_id": "U1", "file_name": "report.txt", "mime_type": "text/plain", "file_size": float64(4)}
	if !reflect.DeepEqual(document["document"], want) || document["photo"] != nil {
		t.Errorf("send-document result %v", document)
	}
	if photo := h.run("messages", "send-photo", "--file", file, "--confirm").object(); photo["photo"] != "big" || photo["document"] != nil {
		t.Errorf("send-photo result %v, want the largest size's file_id", photo)
	}
	if reply := h.run("messages", "send", "--text", "x", "--reply-to", "33", "--confirm").object(); reply["reply_to_message_id"] != float64(33) {
		t.Errorf("send result %v", reply)
	}
}
