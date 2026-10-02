package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/guyravid/ai/cli/tools/telegram/internal/commands"
)

// allowlistHarness is a profile that may address only chat 111111111 (its default) and 222222222.
func allowlistHarness(t *testing.T) *harness {
	h := newHarness(t)
	h.env["TELEGRAM_DEFAULT_CHAT"] = "111111111"
	h.env["TELEGRAM_ALLOWED_CHATS"] = "222222222"
	return h
}

func (r result) detail(key string) any {
	details, _ := r.errorBody()["details"].(map[string]any)
	return details[key]
}

func TestChatOutsideTheAllowlistIsRefusedWithoutARequest(t *testing.T) {
	h := allowlistHarness(t)
	for _, args := range [][]string{
		{"chats", "get", "--chat", "999999999"},
		{"updates", "list", "--chat", "999999999"},
		{"updates", "wait", "--chat", "999999999"},
	} {
		result := h.run(args...)
		if result.exit != 8 || result.errorCode() != "refused" || result.detail("reason") != "chat_not_allowed" || result.detail("chat") != "999999999" {
			t.Errorf("%v: exit %d: %s", args, result.exit, result.raw)
		}
		hint := fmt.Sprint(result.errorBody()["hint"])
		if commands.WritesEnabled && !strings.HasSuffix(hint, "--allow-any-chat") {
			t.Errorf("%v: the full build's hint is the same line plus --allow-any-chat: %q", args, hint)
		}
		if !commands.WritesEnabled && (strings.Contains(hint, "--allow-any-chat ") || !strings.Contains(hint, "allowed_chats")) {
			t.Errorf("%v: the read-only hint says to add the chat to allowed_chats: %q", args, hint)
		}
	}
	if h.fake.total() != 0 {
		t.Errorf("%d upstream requests for refused calls", h.fake.total())
	}
	// DEFAULT_CHAT and ALLOWED_CHATS are both allowed.
	for _, chat := range []string{"111111111", "222222222"} {
		if result := h.run("chats", "get", "--chat", chat); result.exit != 0 && result.exit != 5 {
			t.Errorf("chat %s should be allowed: %s", chat, result.raw)
		}
	}
}

func TestAllowAnyChatLiftsTheRuleOnTheFullBuild(t *testing.T) {
	if !commands.WritesEnabled {
		t.Skip("full build")
	}
	h := allowlistHarness(t)
	h.fake.chats["999999999"] = `{"id":999999999,"type":"private","first_name":"Stranger"}`
	result := h.run("chats", "get", "--chat", "999999999", "--allow-any-chat")
	if result.exit != 0 || result.object()["first_name"] != "Stranger" {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
	if !strings.Contains(h.run("describe", "chats.get").raw, `"allow_any_chat":"--allow-any-chat"`) {
		t.Error("describe should declare the flag in x-cli.flags")
	}
}

func TestAllowAnyChatIsRefusedNotUsageOnTheReadOnlyBuild(t *testing.T) {
	if commands.WritesEnabled {
		t.Skip("read-only build")
	}
	h := allowlistHarness(t)
	for _, args := range [][]string{
		{"chats", "get", "--chat", "999999999", "--allow-any-chat"},
		{"updates", "list", "--allow-any-chat"},
		{"updates", "wait", "--allow-any-chat"},
	} {
		result := h.run(args...)
		if result.exit != 8 || result.errorCode() != "refused" || result.detail("reason") != "any_chat_requires_write_build" {
			t.Errorf("%v: exit %d: %s", args, result.exit, result.raw)
		}
	}
	if h.fake.total() != 0 {
		t.Errorf("%d upstream requests", h.fake.total())
	}
	for _, surface := range []string{h.run("describe").raw, h.run("updates", "list", "--help").raw} {
		if strings.Contains(surface, "allow_any_chat") || strings.Contains(surface, "allow-any-chat") {
			t.Errorf("the read-only build offers --allow-any-chat:\n%.300s", surface)
		}
	}
	// teach says why it is refused, and that it is not a flag to use, but never offers it as a way out.
	if teach := h.run("teach", "chats").raw; !strings.Contains(teach, "any_chat_requires_write_build") || strings.Contains(teach, "lifts the rule") {
		t.Errorf("teach chats on the read-only build: %s", teach)
	}
}

func TestUpdatesListFiltersToTheAllowlistAndCountsWhatItDropped(t *testing.T) {
	h := allowlistHarness(t)
	h.fake.updates = []string{
		message(1, 111111111, time.Hour, "mine"),
		message(2, 222222222, time.Hour, "allowed too"),
		message(3, 333333333, time.Hour, "stranger one"),
		message(4, 333333333, time.Hour, "stranger two"),
		message(5, 444444444, time.Hour, "another stranger"),
	}
	result := h.run("updates", "list")
	if result.exit != 0 || len(result.data()) != 2 {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
	warnings := fmt.Sprint(result.meta()["warnings"])
	if codes := result.warningCodes(); len(codes) != 1 || codes[0] != "filtered_chats" {
		t.Fatalf("warnings %v", codes)
	}
	if !strings.Contains(warnings, "3 update(s) from 2 chat(s)") {
		t.Errorf("the warning counts what was dropped: %s", warnings)
	}
	for _, id := range []string{"333333333", "444444444"} {
		if strings.Contains(result.raw, id) {
			t.Errorf("the warning must not name a dropped chat id (%s): %s", id, result.raw)
		}
	}
	// An explicit --chat narrows to that chat and is not the allowlist filter, so nothing is counted.
	narrowed := h.run("updates", "list", "--chat", "222222222")
	if len(narrowed.data()) != 1 || len(narrowed.warningCodes()) != 0 {
		t.Errorf("narrowed: %s", narrowed.raw)
	}
}

func TestAllowAnyChatShowsEveryChatInUpdatesList(t *testing.T) {
	if !commands.WritesEnabled {
		t.Skip("full build")
	}
	h := allowlistHarness(t)
	h.fake.updates = []string{message(1, 111111111, time.Hour, "mine"), message(2, 333333333, time.Hour, "stranger")}
	result := h.run("updates", "list", "--allow-any-chat")
	if len(result.data()) != 2 || len(result.warningCodes()) != 0 {
		t.Errorf("--allow-any-chat disables the filter: %s", result.raw)
	}
}

func TestEmptyAllowlistFiltersEverythingAndSaysSo(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "TELEGRAM_ALLOWED_CHATS")
	h.fake.updates = []string{message(1, 111111111, time.Hour, "hello")}
	result := h.run("updates", "list")
	if result.exit != 0 || len(result.data()) != 0 {
		t.Fatalf("exit %d: %s", result.exit, result.raw)
	}
	codes := result.warningCodes()
	if len(codes) != 2 || codes[0] != "no_allowed_chats" || codes[1] != "filtered_chats" {
		t.Fatalf("warnings %v", codes)
	}
	message := fmt.Sprint(result.meta()["warnings"])
	if !strings.Contains(message, "allowed_chats") || strings.Contains(message, "111111111") {
		t.Errorf("the warning names the fix, not ids: %s", message)
	}
	if hasAnyChat := strings.Contains(message, "--allow-any-chat"); hasAnyChat != commands.WritesEnabled {
		t.Errorf("--allow-any-chat is named only where it exists (full build %v): %s", commands.WritesEnabled, message)
	}
}
