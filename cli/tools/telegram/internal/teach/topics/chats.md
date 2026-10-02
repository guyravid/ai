---
summary: Chat ids, public @usernames, the default chat, and why a chat may be unreachable
---
## Chat ids

A chat is addressed by its id, kept as a string everywhere: private chats use the person's user id
(a positive number), groups and channels use negative numbers such as `-1001234567890`, and a public
channel can also be addressed by its `@username`. Anything else is rejected as `usage` (a flag) or
`config` (a setting).

## Which chat a command uses

Commands that address one chat take `--chat <id>`. Without it they use the profile's `default_chat`
setting, and with neither they fail with `usage`. `{{tool}} list-config` shows the `DEFAULT_CHAT` in
effect and where it came from.

- `{{tool}} chats get` shows the default chat; `{{tool}} chats get --chat <id>` shows another.
- `updates list` accepts `--chat <id>` to keep only that chat's updates.

## The allowlist

Each profile may address only its allowed chats: `default_chat` plus `allowed_chats` (a
comma-separated list in the config file, or `TELEGRAM_ALLOWED_CHATS`). `default_chat` is always allowed.
A profile that sets neither allows no chat.

- Addressing a chat outside the set is `refused` with `details.reason: "chat_not_allowed"` and
  `details.chat`; nothing is sent to Telegram. This applies to `chats get`, `updates list --chat`,
  `updates wait --chat`{{#writes}} and every send{{/writes}}.
- `updates list` and `updates wait` without `--chat` do not fail: the allowlist filters what they show.
  Updates from other chats are dropped and counted in the warning `filtered_chats` (a count, never
  the ids). An empty allowlist filters out everything and adds the warning `no_allowed_chats`.
  `updates wait` listens to `default_chat` when there is one, otherwise to every allowed chat.
{{#writes}}
- `--allow-any-chat` lifts the rule for one call: any chat can be addressed, and `updates list
  --allow-any-chat` shows every chat that wrote to the bot. It exists only in the full build. Use it
  deliberately: it is how a message can be sent to a chat nobody listed.
{{/writes}}
- A read-only build has no `--allow-any-chat`: naming it is `refused` with `details.reason:
  "any_chat_requires_write_build"`, not a usage error. Add the chat to `allowed_chats` instead.

A profile that overrides only `bot_token_file` inherits the default bot's `default_chat` and
`allowed_chats`, so check `{{tool}} list-config --profile <name>` before using it on a new chat.

## Finding a chat id

Ask the person to message the bot, then read the queue. The `chat.id` of their message is the id to
use. Updates from chats not on the allowlist are filtered out, so an unknown chat is invisible until
it is allowed{{#writes}}; run `{{tool}} updates list --allow-any-chat --fields
update_id,chat.id,chat.type,from.username` to see every chat{{/writes}}.
Then add the id to `allowed_chats`.

## Unreachable chats

A bot can only message someone who has opened it and pressed Start, and only a group it belongs to.
Telegram answers 403 otherwise, reported as the tool-specific error `chat_unreachable` (exit 32,
not retriable). The fix is on the recipient's side: open the bot and press Start, or add the bot to
the group again. `not_found` on `chats get` means the id is wrong, or the bot has never seen that
chat.
