package commands

import (
	"github.com/guyravid/ai/cli/tools/telegram/internal/config"
	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
	"github.com/guyravid/ai/cli/tools/telegram/internal/registry"
	"github.com/guyravid/ai/cli/tools/telegram/internal/telegram"
)

// AllowedChats is the set a profile may address without --allow-any-chat: DEFAULT_CHAT plus
// ALLOWED_CHATS. It is empty when neither is set.
func AllowedChats(call *registry.Call) []string {
	var allowed []string
	if call.DefaultChat != "" {
		allowed = append(allowed, call.DefaultChat)
	}
	return append(allowed, call.AllowedChats...)
}

// checkFormat validates a --chat value, so a malformed one never reaches upstream.
func checkFormat(flag string) *errs.Error {
	if config.ValidChatID(flag) {
		return nil
	}
	return errs.Usagef("--chat %q is not a chat id (digits, optionally negative) or a public @username.", flag).
		WithHint("telegram teach chats").WithDetail("flag", "--chat")
}

// refuseChat is the refusal for a chat outside the allowed set. The way out depends on the build:
// --allow-any-chat exists only in the full build.
func refuseChat(call *registry.Call, chat string) *errs.Error {
	refusal := errs.New(errs.Refused, "Chat %s is not one this profile may address.", chat).
		WithDetail("reason", "chat_not_allowed").WithDetail("chat", chat)
	if WritesEnabled {
		return refusal.WithHint(call.CommandLine("--allow-any-chat"))
	}
	return refusal.WithHint("Add the chat to allowed_chats in the config file (telegram teach chats); this read-only build has no --allow-any-chat.")
}

// ResolveChat picks the chat a chat-scoped command addresses (plan, "Allowlist rule"): --chat if
// given, else DEFAULT_CHAT, else usage. The chat must be in the allowed set unless allowAny, which
// only the full build can set; otherwise the call is refused.
func ResolveChat(call *registry.Call, flag string, allowAny bool) (string, *errs.Error) {
	chat := flag
	if flag != "" {
		if err := checkFormat(flag); err != nil {
			return "", err
		}
	} else if chat = call.DefaultChat; chat == "" {
		return "", errs.Usagef("No chat: pass --chat or set default_chat.").
			WithHint("telegram list-config").WithDetail("missing", "chat")
	}
	if !allowAny && !telegram.ChatInSet(chat, AllowedChats(call)) {
		return "", refuseChat(call, chat)
	}
	return chat, nil
}

// ChatScope is the scope of a command that reads updates (plan, "Allowlist rule"). An explicit
// --chat must be allowed and keeps only that chat. Without it the allowed set filters the queue,
// unless allowAny lifts the filter. wait narrows to the default chat when there is one, because
// message ids mean something only inside one chat.
func ChatScope(call *registry.Call, flag string, allowAny, defaultNarrows bool) (telegram.ChatScope, *errs.Error) {
	if flag != "" {
		chat, err := ResolveChat(call, flag, allowAny)
		return telegram.ChatScope{Chat: chat}, err
	}
	if defaultNarrows && call.DefaultChat != "" {
		chat, err := ResolveChat(call, "", allowAny)
		return telegram.ChatScope{Chat: chat}, err
	}
	return telegram.ChatScope{Allowed: AllowedChats(call), FilterAllowed: !allowAny}, nil
}
