---
summary: The bot's update queue: what reading shows, the 100-update window, and one reader at a time
---
## What an update is

Telegram holds everything sent to the bot as a queue of updates, each with a growing `update_id`.
`{{tool}} updates list` flattens each into one record: `update_id`, `type` (`message`,
`edited_message`, `channel_post`, `callback_query`, `my_chat_member`, ...), `date`, `chat`, `from`,
`message_id`, `text`, `caption`, `reply_to_message_id`, `document` (`file_id`, `file_unique_id`, `file_name`, `mime_type`, `file_size`), and `photo` (the
`file_id` of the largest size). Dates are RFC 3339 UTC. The default fields are
`update_id,type,date,chat.id,from.username,message_id,text`.

## Reading never removes anything

`updates list` reads the queue as it is and filters locally by `--chat`, `--after <update_id>`, and
the time window (`--since`, default the last 24 hours; check `meta.window.source`). It never
confirms updates, so reading twice shows the same updates. Page with `--limit` and `--cursor`; the
cursor is the last `update_id` delivered.

## The 100-update window

Telegram shows at most the oldest 100 unconfirmed updates. When exactly 100 come back, newer ones
exist but cannot be seen until earlier ones are confirmed, and the response carries the warning
`queue_window_full`. Telegram also drops unconfirmed updates after 24 hours, so the queue is not a
message history: the Bot API cannot read a chat's past messages.

## One reader at a time

Only one consumer can read a bot's updates. If another process is long polling the same bot, or a
webhook is set on it, Telegram answers 409 and the tool reports `conflict`. Stop the other poller or
remove the webhook, then retry.

## Waiting for a reply

`{{tool}} updates wait` blocks until a message arrives and returns it. A match is a `message` (or
`channel_post`) in the chat being waited on:

- `--after-message <id>`: its `message_id` is greater than `id`.
- `--reply-to <id>`: it is a reply to message `id`.
- Both: both must hold. Neither: any message newer than the newest update already in the queue when
  the wait began, so old, unread updates never count.

`--max-wait` is how long to wait: `60s` by default, `1h` at most. Without a match the answer is
still a success, `ok:true` with `data:[]` and the warning `no_reply`; that tells it apart from
"nothing to read". The call's default `--budget` is `--max-wait` plus 30 seconds, and a `--budget`
shorter than `--max-wait` is `usage`.

It never removes updates and never sends an offset. It long polls Telegram only while the queue is
empty (a long poll returns at once when anything is pending, even an old update that does not match),
and otherwise sleeps `POLL_INTERVAL` (2 s) between polls. If 100 updates are pending and none match it
stops with `queue_window_full`, because newer ones are invisible until earlier ones are acknowledged.
Only one reader of the bot's updates can run at a time: a second one gives `conflict`.
{{#writes}}

## Acknowledging updates

`{{tool}} updates ack --through <update_id> --confirm` tells Telegram that every update up to and
including `update_id` is handled, and **permanently deletes them for every reader of the bot**, not just
this tool. It is the only command that sends an offset, it cannot be undone, and an unconfirmed one is
refused with a preview showing the offset it would send. `data` is `{acked_through, has_more}`.

Use it when `queue_window_full` hides newer updates, or when a long-lived process has handled
everything up to an id. Do not use it to tidy up: reading never needs it, and unacknowledged updates
expire after 24 hours anyway.
{{/writes}}
