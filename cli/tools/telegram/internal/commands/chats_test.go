package commands

import (
	"strings"
	"testing"
	"time"

	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
	"github.com/guyravid/ai/cli/tools/telegram/internal/registry"
)

func testCall(defaultChat string, allowed ...string) *registry.Call {
	return &registry.Call{DefaultChat: defaultChat, AllowedChats: allowed,
		CommandLine: func(extra ...string) string {
			return strings.Join(append([]string{"telegram chats get"}, extra...), " ")
		}}
}

func TestResolveChat(t *testing.T) {
	cases := []struct {
		name, flag, defaultChat string
		allowed                 []string
		allowAny                bool
		want                    string
		code                    errs.Code
	}{
		{name: "flag wins over default when allowed", flag: "-1001234567890", defaultChat: "111", allowed: []string{"-1001234567890"}, want: "-1001234567890"},
		{name: "default when no flag", defaultChat: "111", want: "111"},
		{name: "the default chat is allowed without being listed", flag: "111", defaultChat: "111", want: "111"},
		{name: "public username, any case", flag: "@News_Channel", allowed: []string{"@news_channel"}, want: "@News_Channel"},
		{name: "no chat anywhere is usage", code: errs.Usage},
		{name: "malformed flag is usage", flag: "abc", defaultChat: "111", code: errs.Usage},
		{name: "a malformed flag is not rescued by the default", flag: "1 2", defaultChat: "111", code: errs.Usage},
		{name: "a chat outside the allowed set is refused", flag: "999", defaultChat: "111", allowed: []string{"222"}, code: errs.Refused},
		{name: "with no allowed set at all, an explicit chat is refused", flag: "999", code: errs.Refused},
		{name: "allow-any lifts the rule", flag: "999", defaultChat: "111", allowAny: true, want: "999"},
		{name: "allow-any does not excuse a malformed id", flag: "x", allowAny: true, code: errs.Usage},
		{name: "leading zeros are the same chat", flag: "0111", defaultChat: "111", want: "0111"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveChat(testCall(tc.defaultChat, tc.allowed...), tc.flag, tc.allowAny)
			if tc.code != "" {
				if err == nil || err.Code != tc.code {
					t.Fatalf("got %q, %v; want %s", got, err, tc.code)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestChatNotAllowedRefusalNamesTheReasonAndTheWayOut(t *testing.T) {
	_, err := ResolveChat(testCall("111"), "999", false)
	if err == nil || err.Code != errs.Refused || err.Details["reason"] != "chat_not_allowed" || err.Details["chat"] != "999" {
		t.Fatalf("got %v", err)
	}
	if WritesEnabled && !strings.Contains(err.Hint, "--allow-any-chat") {
		t.Errorf("the full build's hint is the same command with --allow-any-chat: %q", err.Hint)
	}
	if !WritesEnabled && (strings.Contains(err.Hint, "--allow-any-chat ") || !strings.Contains(err.Hint, "allowed_chats")) {
		t.Errorf("the read-only build's hint is to add the chat to allowed_chats: %q", err.Hint)
	}
}

func TestChatScope(t *testing.T) {
	call := testCall("111", "222")
	cases := []struct {
		name, flag       string
		allowAny, narrow bool
		chat             string
		filter           bool
		allowed          []string
		code             errs.Code
	}{
		{name: "no flag filters the queue to the allowed set", filter: true, allowed: []string{"111", "222"}},
		{name: "allow-any lifts the filter", allowAny: true, allowed: []string{"111", "222"}},
		{name: "an allowed --chat narrows to it", flag: "222", chat: "222"},
		{name: "a disallowed --chat is refused", flag: "333", code: errs.Refused},
		{name: "allow-any with --chat still narrows to it", flag: "333", allowAny: true, chat: "333"},
		{name: "wait narrows to the default chat", narrow: true, chat: "111"},
		{name: "wait with allow-any and a default still narrows to it", narrow: true, allowAny: true, chat: "111"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scope, err := ChatScope(call, tc.flag, tc.allowAny, tc.narrow)
			if tc.code != "" {
				if err == nil || err.Code != tc.code {
					t.Fatalf("got %+v, %v; want %s", scope, err, tc.code)
				}
				return
			}
			if err != nil || scope.Chat != tc.chat || scope.FilterAllowed != tc.filter || strings.Join(scope.Allowed, ",") != strings.Join(tc.allowed, ",") {
				t.Fatalf("got %+v, %v", scope, err)
			}
		})
	}
	if scope, _ := ChatScope(testCall(""), "", false, true); !scope.FilterAllowed || len(scope.Allowed) != 0 {
		t.Errorf("wait with no default chat falls back to the allowlist filter: %+v", scope)
	}
}

func TestReadCommandsAreRegisteredAndNotMutating(t *testing.T) {
	names := map[string]bool{}
	for _, command := range Reads() {
		names[command.Name] = true
		if command.IsWrite() {
			t.Errorf("%s must not be a write", command.Name)
		}
		if command.Collectable {
			t.Errorf("%s: nothing is collectable (datasets are dropped)", command.Name)
		}
	}
	for _, want := range []string{"bot.get", "chats.get", "updates.list"} {
		if !names[want] {
			t.Errorf("missing %s", want)
		}
	}
}

func TestWaitBudget(t *testing.T) {
	cases := []struct {
		name       string
		maxWait    string
		configured time.Duration
		explicit   bool
		want       time.Duration
		usage      bool
	}{
		{name: "default max-wait gives 60s + 30s", want: 90 * time.Second, configured: 60 * time.Second},
		{name: "a longer wait grows the default", maxWait: "5m", want: 5*time.Minute + 30*time.Second, configured: 60 * time.Second},
		{name: "an explicit budget at least max-wait is kept", maxWait: "90s", explicit: true, configured: 2 * time.Minute, want: 2 * time.Minute},
		{name: "an explicit budget equal to max-wait is allowed", maxWait: "90s", explicit: true, configured: 90 * time.Second, want: 90 * time.Second},
		{name: "an explicit budget below max-wait is usage", maxWait: "90s", explicit: true, configured: 30 * time.Second, usage: true},
		{name: "an explicit budget below the default max-wait is usage", explicit: true, configured: 10 * time.Second, usage: true},
		{name: "max-wait over an hour is usage", maxWait: "61m", configured: time.Minute, usage: true},
		{name: "max-wait of exactly an hour is allowed", maxWait: "1h", configured: time.Minute, want: time.Hour + 30*time.Second},
		{name: "max-wait without units is usage", maxWait: "60", configured: time.Minute, usage: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := waitBudget(updatesWaitInput{MaxWait: tc.maxWait}, tc.configured, tc.explicit)
			if tc.usage {
				if typed, ok := err.(*errs.Error); !ok || typed.Code != errs.Usage {
					t.Fatalf("got %v, %v; want usage", got, err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}
