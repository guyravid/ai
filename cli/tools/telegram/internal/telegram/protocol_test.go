package telegram

import (
	"strings"
	"testing"
	"time"

	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
)

func TestStatusErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		code   errs.Code
		exit   int
	}{
		{"chat not found", 400, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`, errs.NotFound, 5},
		{"chat not found, any case", 400, `{"ok":false,"error_code":400,"description":"BAD REQUEST: CHAT NOT FOUND"}`, errs.NotFound, 5},
		{"reply target missing", 400, `{"ok":false,"error_code":400,"description":"Bad Request: message to reply not found"}`, errs.NotFound, 5},
		{"too long", 400, `{"ok":false,"error_code":400,"description":"Bad Request: message is too long"}`, errs.Validation, 6},
		{"entities", 400, `{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities: unclosed tag"}`, errs.Validation, 6},
		{"unauthorized", 401, `{"ok":false,"error_code":401,"description":"Unauthorized"}`, errs.Auth, 4},
		{"blocked", 403, `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`, errs.ChatUnreachable, 32},
		{"never started", 403, `{"ok":false,"error_code":403,"description":"Forbidden: bot can't initiate conversation with a user"}`, errs.ChatUnreachable, 32},
		{"unknown path", 404, `{"ok":false,"error_code":404,"description":"Not Found"}`, errs.Auth, 4},
		{"other consumer", 409, `{"ok":false,"error_code":409,"description":"Conflict: terminated by other getUpdates request; make sure that only one bot instance is running"}`, errs.Conflict, 7},
		{"webhook", 409, `{"ok":false,"error_code":409,"description":"Conflict: can't use getUpdates method while webhook is active"}`, errs.Conflict, 7},
		{"too large", 413, `{"ok":false,"error_code":413,"description":"Request Entity Too Large"}`, errs.Validation, 6},
		{"throttled", 429, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 7","parameters":{"retry_after":7}}`, errs.RateLimited, 9},
		{"server", 502, `{"ok":false,"error_code":502,"description":"Bad Gateway"}`, errs.Upstream, 12},
		{"server, not JSON", 503, `<html>maintenance</html>`, errs.Upstream, 12},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Protocol{}.StatusError(tc.status, []byte(tc.body), 0, nil)
			if err.Code != tc.code || err.ExitCode() != tc.exit {
				t.Fatalf("got %s (exit %d), want %s (exit %d)", err.Code, err.ExitCode(), tc.code, tc.exit)
			}
			if err.Details["upstream_status"] != tc.status {
				t.Errorf("details %v", err.Details)
			}
		})
	}
}

func TestChatUnreachableIsNotRetriableAndSaysWhoFixesIt(t *testing.T) {
	err := Protocol{}.StatusError(403, []byte(`{"ok":false,"error_code":403,"description":"Forbidden"}`), 0, nil)
	if err.Retriable() {
		t.Error("chat_unreachable must not be retriable")
	}
	if !strings.Contains(err.Hint, "Start") {
		t.Errorf("hint %q should say the recipient must press Start", err.Hint)
	}
}

func TestRateLimitedCarriesRetryAfterMilliseconds(t *testing.T) {
	err := Protocol{}.StatusError(429, []byte(`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":7}}`), 0, nil)
	if err.RetryAfterMs == nil || *err.RetryAfterMs != 7000 {
		t.Fatalf("retry_after_ms = %v", err.RetryAfterMs)
	}
	// The header is the fallback when the body has no parameters.
	err = Protocol{}.StatusError(429, []byte(`nope`), 3*time.Second, nil)
	if err.RetryAfterMs == nil || *err.RetryAfterMs != 3000 {
		t.Fatalf("header fallback: retry_after_ms = %v", err.RetryAfterMs)
	}
}

func TestConflictHintNamesBothCauses(t *testing.T) {
	err := Protocol{}.StatusError(409, []byte(`{"ok":false,"error_code":409,"description":"Conflict"}`), 0, nil)
	text := strings.ToLower(err.Message + " " + err.Hint)
	if !strings.Contains(text, "webhook") || !strings.Contains(text, "poller") {
		t.Errorf("message and hint should name another poller and a webhook: %s", text)
	}
}

// Upstream text copied into details is cut to 512 characters (contract §3.3).
func TestDescriptionIsCut(t *testing.T) {
	body := `{"ok":false,"error_code":400,"description":"` + strings.Repeat("é", 3000) + `"}`
	err := Protocol{}.StatusError(400, []byte(body), 0, nil)
	description, _ := err.Details["upstream_description"].(string)
	if count := len([]rune(description)); count != 512 {
		t.Errorf("description is %d characters, want 512", count)
	}
}

func TestResultUnwrapsAndRejectsOkFalse(t *testing.T) {
	value, err := Protocol{}.Result(200, []byte(`{"ok":true,"result":{"id":123456789,"username":"my_bot"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if username, _ := value.Get("username"); username.Text() != "my_bot" {
		t.Errorf("result %s", value.Marshal())
	}
	// Numbers pass through as written.
	if id, _ := value.Get("id"); string(id.Raw) != "123456789" {
		t.Errorf("id %s", id.Raw)
	}
	_, err = Protocol{}.Result(200, []byte(`{"ok":false,"error_code":403,"description":"Forbidden"}`))
	if err == nil || err.Code != errs.ChatUnreachable {
		t.Errorf("a 200 that says ok:false maps like its error_code, got %v", err)
	}
	_, err = Protocol{}.Result(200, []byte(`<html>`))
	if err == nil || err.Code != errs.Upstream {
		t.Errorf("non-JSON should be upstream, got %v", err)
	}
}

func TestChatParam(t *testing.T) {
	if got := ChatParam("-1001234567890"); got != int64(-1001234567890) {
		t.Errorf("numeric ids are sent as numbers, got %#v", got)
	}
	if got := ChatParam("@news_channel"); got != "@news_channel" {
		t.Errorf("usernames are sent as strings, got %#v", got)
	}
	if got := ChatParam("99999999999999999999"); got != "99999999999999999999" {
		t.Errorf("an id beyond int64 stays a string, got %#v", got)
	}
}
