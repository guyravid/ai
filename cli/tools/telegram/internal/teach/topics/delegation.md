---
summary: Ask a person something and wait for the answer: the send, then wait loop with a worked example
writes: true
---
## The loop

To delegate a question to a person and read their answer:

1. `{{tool}} messages send --text "<question>" --confirm` returns the sent message, including its
   `message_id`.
2. `{{tool}} updates wait --after-message <message_id> --max-wait 5m` blocks until a message newer
   than that arrives in the same chat, and returns it.

`--after-message` is the simple form: any later message in the chat counts. Use `--reply-to
<message_id>` instead when the person may be answering several things and only a reply to this
question should match. Ids are per chat, so wait in the chat you sent to (`--chat`, or the same
`default_chat`).

## Worked example

```
$ telegram messages send --text "Deploy finished. Roll back? Reply yes or no." --confirm
{"ok":true,"command":"messages.send","data":{"message_id":4021,"date":"2026-10-02T09:15:03Z","chat":{"id":111111111,"type":"private"},"text":"Deploy finished. Roll back? Reply yes or no."},...}

$ telegram updates wait --after-message 4021 --max-wait 5m --fields update_id,message_id,text
{"ok":true,"command":"updates.wait","data":[{"update_id":812346,"message_id":4022,"text":"no"}],...}
```

The answer is `data[0].text`. If nobody replies in time, `data` is `[]` with the warning `no_reply`:
decide whether to wait again (the question is still there) or give up. A wait never removes updates,
so repeating it is safe.

## Things that go wrong

- The recipient has not pressed Start on the bot: the send fails with `chat_unreachable` (exit 32).
- A send that times out has an unknown outcome (`details.write_state: "unknown"`). Do not send again
  blindly: check the chat first, because the message may have arrived.
- Another process reading the bot's updates, or a webhook, gives `conflict` on the wait.
- Sends are never retried. A `rate_limited` answer carries `retry_after_ms`; wait that long, then send.
