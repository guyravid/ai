//go:build readonly

package commands

import (
	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
	"github.com/guyravid/ai/cli/tools/telegram/internal/registry"
)

// WritesEnabled reports whether this build includes write commands.
const WritesEnabled = false

// Writes returns nothing in a read-only build: the write commands are not compiled in.
func Writes() []*registry.Command { return nil }

// anyChatUnavailable removes --allow-any-chat from every chat-scoped command. Naming it is refused,
// not a usage error: the flag exists, this build does not offer it (plan, "Allowlist rule").
func anyChatUnavailable() []registry.Unavailable {
	return []registry.Unavailable{{Name: "allow_any_chat", Err: func() *errs.Error {
		return errs.New(errs.Refused, "--allow-any-chat is not available in this read-only build.").
			WithHint("Add the chat to allowed_chats in the config file (telegram teach chats), or use the full build.").
			WithDetail("reason", "any_chat_requires_write_build").WithDetail("flag", "--allow-any-chat")
	}}}
}
