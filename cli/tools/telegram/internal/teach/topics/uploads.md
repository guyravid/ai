---
summary: Sending local files as documents or photos: limits, previews, and the exfiltration risk
writes: true
---
## Sending a file

`{{tool}} messages send-document --file <path> [--caption <text>] --confirm` uploads a file as a
document (up to 50 MB). `{{tool}} messages send-photo --file <path>` uploads an image as a photo
(up to 10 MB; Telegram recompresses it, so use `send-document` to keep the original). Both take
`--chat`, `--parse-mode` (for the caption), `--reply-to`, and `--silent`, and return the sent message
like `messages send`. The result also carries what Telegram stored, for reuse: `document` (`file_id`,
`file_unique_id`, `file_name`, `mime_type`, `file_size`) after `send-document`, and `photo` (the
`file_id` of the largest size) after `send-photo`. Fields Telegram did not return are absent. Captions are at most 1024 characters. There is no local size check: Telegram's
rejection of a too-large or malformed file is `validation`.

The file must be a regular file. A symlink is resolved, and the preview shows the path it resolved
to; a directory, device, or missing file is `usage`.

## Previews never show file contents

Without `--confirm`, or with `--dry-run`, the preview shows the method, the URL with the token
replaced by `bot<redacted>`, and the form fields, with the file as `{path, bytes, name}`. The content
is read only when the upload is actually sent.

## The risk

Any file the tool's user can read can be sent to any chat the profile may address, and to any chat at
all with `--allow-any-chat`. An agent that is tricked or mistaken can therefore send a private file
(a key, a config, an `.env`) to a chat outside your control. Keep `allowed_chats` as small as
possible, read the preview's `path` before confirming, and where an agent should only read, use the
read-only build `telegram-ro`: it has no send commands at all. The CLI cannot judge a file's
sensitivity.
