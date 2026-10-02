# Envelope Wire Format

The machine-readable companion to `../../contract/CONTRACT.md` §3, §4, §7, §9, and §10: full JSON
Schemas, worked examples, and the cursor format. An implementation in any language can match the
wire format from this file alone.

This file adds detail; it never adds rules. Where it seems to say something the contract does not,
the contract wins.

## Contents

1. [Envelope schema](#1-envelope-schema)
2. [Page schema](#2-page-schema)
3. [Examples: lists and paging](#3-examples-lists-and-paging)
4. [Examples: datasets](#4-examples-datasets)
5. [Examples: one per error code](#5-examples-one-per-error-code)
6. [Examples: writes](#6-examples-writes)
7. [Diagnostics payloads](#7-diagnostics-payloads)
8. [Cursor format](#8-cursor-format)
9. [Warning codes](#9-warning-codes)

---

## 1. Envelope schema

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "agentcli/envelope.json",
  "type": "object",
  "required": ["ok", "tool", "command", "data", "meta"],
  "additionalProperties": false,
  "properties": {
    "ok": { "type": "boolean" },
    "tool": { "type": "string" },
    "command": { "type": ["string", "null"], "pattern": "^[a-z][a-z0-9-]*(\\.[a-z][a-z0-9-]*)*$" },
    "data": true,
    "error": {
      "type": "object",
      "required": ["code", "exit_code", "message", "retriable"],
      "additionalProperties": false,
      "properties": {
        "code": { "enum": ["internal", "usage", "config", "auth", "not_found", "validation", "conflict",
                           "refused", "rate_limited", "timeout", "network", "upstream", "partial",
                           "cache_miss", "canceled"] },
        "exit_code": { "type": "integer" },
        "message": { "type": "string", "minLength": 1, "maxLength": 512 },
        "retriable": { "type": "boolean" },
        "retry_after_ms": { "type": "integer", "minimum": 0 },
        "hint": { "type": "string" },
        "details": { "type": "object" }
      }
    },
    "meta": {
      "type": "object",
      "required": ["contract_version", "tool_version"],
      "properties": {
        "contract_version": { "type": "string", "pattern": "^\\d+\\.\\d+$" },
        "tool_version": { "type": "string" },
        "page": { "$ref": "agentcli/page.json" },
        "window": {
          "type": "object",
          "required": ["since", "until", "source"],
          "properties": {
            "since": { "type": "string", "format": "date-time" },
            "until": { "type": "string", "format": "date-time" },
            "source": { "enum": ["flag", "default", "cursor"] }
          }
        },
        "fields": { "type": "array", "items": { "type": "string" } },
        "stripped_empty": { "const": true },
        "elided_fields": { "type": "array", "items": { "type": "string" } },
        "sort": { "type": "string" },
        "dataset": { "type": "object" },
        "errors": { "type": "array", "items": { "type": "object" } },
        "dry_run": { "const": true },
        "warnings": {
          "type": "array", "minItems": 1,
          "items": { "type": "object", "required": ["code", "message"],
                     "properties": { "code": { "type": "string" }, "message": { "type": "string" } } }
        },
        "timing": { "type": "object" }
      }
    }
  },
  "allOf": [
    { "if": { "properties": { "ok": { "const": true } } },
      "then": { "not": { "required": ["error"] } } },
    { "if": { "properties": { "ok": { "const": false } } },
      "then": { "required": ["error"] } }
  ]
}
```

A command's full output schema is this schema with `properties.data` replaced by
`{"anyOf":[<outputSchema>,{"type":"null"}]}`: a failure carries `data:null`. That substitution is what
an MCP server does to publish a self-contained schema (contract §6.2, §16.2). The `outputSchema`
itself must already be open, as contract §6.2 requires: no `required`, no
`additionalProperties:false`, `"<depth-elided>"` admitted below the record, and no string constraints.

Checks the schema cannot express, to cover in tests:

| Check | Rule |
|---|---|
| Exit agreement | `error.exit_code` equals the process exit status, and the pair matches the table in §5 |
| Data on failure | `data` is `null` on failure, except `partial` (records retrieved) and `doctor` (the check report) |
| Details size | `error.details` serializes to at most 2048 bytes |
| No timing leakage | Without `--timing`, `meta` holds no timestamps, durations, or request ids |
| Empty warnings | `warnings` is absent rather than empty |

## 2. Page schema

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "agentcli/page.json",
  "type": "object",
  "required": ["limit", "count", "total", "total_is_exact", "has_more", "next_cursor", "truncated"],
  "additionalProperties": false,
  "properties": {
    "limit": { "type": "integer", "minimum": 1 },
    "count": { "type": "integer", "minimum": 0 },
    "total": { "type": ["integer", "null"], "minimum": 0 },
    "total_is_exact": { "type": "boolean" },
    "has_more": { "type": "boolean" },
    "next_cursor": { "type": ["string", "null"] },
    "truncated": { "type": "boolean" },
    "truncated_reason": { "enum": ["max_pages", "max_bytes", "max_bytes_below_minimum", "budget"] },
    "dropped": { "type": "integer", "minimum": 1 }
  }
}
```

The nullable members are still required: the contract asks for the same members every time, so an
agent's loop never checks whether a key exists.

| Check | Rule |
|---|---|
| Cursor agreement | `next_cursor` is `null` exactly when `has_more` is `false` |
| Count bound | `count <= limit` |
| Truncation | `truncated` is `true` exactly when `truncated_reason` is present, and then `count < limit` and `has_more` is `true` |
| Exactness | `total` is `null` implies `total_is_exact` is `false` |

## 3. Examples: lists and paging

### One call fills the limit

```console
$ trello cards list --board 5f2a --limit 60 --fields id,name,due
```

Upstream returns 20 cards per page, so the tool fetched three pages.

```json
{"ok":true,"tool":"trello","command":"cards.list","data":["… 60 records …"],"meta":{"page":{"limit":60,"count":60,"total":214,"total_is_exact":true,"has_more":true,"next_cursor":"eyJ2IjoxLCJjIjoiY2FyZHMubGlzdCIsInUiOiI2MSIsInEiOnsiYm9hcmQiOiI1ZjJhIiwiZmllbGRzIjoiaWQsbmFtZSxkdWUiLCJsaW1pdCI6NjB9fQ","truncated":false},"fields":["id","name","due"],"stripped_empty":true,"sort":"dateLastActivity desc, id asc","contract_version":"1.4","tool_version":"0.3.0"}}
```

### Continuing with the cursor alone

```console
$ trello cards list --cursor eyJ2IjoxLCJjIjoiY2FyZHMubGlzdCIs…
```

No `--board`, `--fields`, or `--limit`: the cursor carries them.

### A cap stops the tool early

```console
$ trello cards list --board 5f2a --limit 100 --fields '*'
```

Full records are large, so the byte cap binds after 41.

```json
{"ok":true,"tool":"trello","command":"cards.list","data":["… 41 records …"],"meta":{"page":{"limit":100,"count":41,"total":214,"total_is_exact":true,"has_more":true,"next_cursor":"eyJ2IjoxLCJjIjoiY2FyZHMubGlzdCIs…","truncated":true,"truncated_reason":"max_bytes","dropped":19},"fields":["*"],"stripped_empty":true,"sort":"dateLastActivity desc, id asc","contract_version":"1.4","tool_version":"0.3.0"}}
```

Exit 0. The tool fetched 60 records, fit 41 under `--max-bytes`, and dropped 19 from the end.
`next_cursor` resumes at the first dropped record, so nothing is skipped.

### Time window

```console
$ trello actions list --board 5f2a
```

```json
{"ok":true,"tool":"trello","command":"actions.list","data":["…"],"meta":{"page":{"limit":25,"count":25,"total":null,"total_is_exact":false,"has_more":true,"next_cursor":"eyJ2Ijox…","truncated":false},"window":{"since":"2026-09-29T09:00:00Z","until":"2026-09-30T09:00:00Z","source":"default"},"sort":"date desc, id asc","contract_version":"1.4","tool_version":"0.3.0"}}
```

`source:"default"` says the 24-hour window was chosen by the tool, not the agent. An agent looking for
something older knows to pass `--since`.

## 4. Examples: datasets

### Collect

```console
$ trello cards list --board 5f2a --all --fields id,name,list,due --limit 25
```

```json
{"ok":true,"tool":"trello","command":"cards.list","data":["… 25 records …"],"meta":{"page":{"limit":25,"count":25,"total":4213,"total_is_exact":true,"has_more":true,"next_cursor":"eyJ2IjoxLCJjIjoiZGF0YXNldC5yZWFkIiwiZCI6IjNjOWUyYTRmLTdiMWQtNGU4YS05ZjYwLTJkNWM4YjFhN2UzNCIsIm8iOjI1LCJxIjp7ImxpbWl0IjoyNX19","truncated":false},"dataset":{"id":"3c9e2a4f-7b1d-4e8a-9f60-2d5c8b1a7e34","path":"/Users/g/Library/Caches/agentcli/trello/datasets/3c9e2a4f-7b1d-4e8a-9f60-2d5c8b1a7e34.jsonl","record_count":4213,"bytes":612004,"complete":true,"offset":0,"returned":25,"ttl_seconds":3600,"read_hint":"trello dataset read 3c9e2a4f-7b1d-4e8a-9f60-2d5c8b1a7e34 --offset 25 --limit 100"},"fields":["id","name","list","due"],"stripped_empty":true,"sort":"dateLastActivity desc, id asc","contract_version":"1.4","tool_version":"0.3.0"}}
```

`--limit 25` sized the response, not the collection. The tool fetched all 4213 records, in the
largest pages upstream allows. `next_cursor` is a cursor for `dataset read`.

The agent now has three ways forward:

| Want | Call |
|---|---|
| The next part | `trello dataset read 3c9e… --cursor <next_cursor>` or the `read_hint` |
| A specific slice | `trello dataset read 3c9e… --offset 2000 --limit 50` |
| To process the whole file locally | Read `path` with line tools, such as `jq -c 'select(.due != null)' <path>` |

### Read

```json
{"ok":true,"tool":"trello","command":"dataset.read","data":["… 100 records …"],"meta":{"page":{"limit":100,"count":100,"total":4213,"total_is_exact":true,"has_more":true,"next_cursor":"eyJ2IjoxLCJjIjoiZGF0YXNldC5yZWFkIiwiZCI6IjNjOWUyYTRmLTdiMWQtNGU4YS05ZjYwLTJkNWM4YjFhN2UzNCIsIm8iOjEyNSwicSI6eyJsaW1pdCI6MTAwfX0","truncated":false},"dataset":{"id":"3c9e2a4f-7b1d-4e8a-9f60-2d5c8b1a7e34","complete":true,"offset":25,"returned":100,"ttl_seconds":3600},"fields":["id","name","list","due"],"contract_version":"1.4","tool_version":"0.3.0"}}
```

No upstream request. The read reset the dataset's lifetime.

### Collection stopped by a cap

```json
"dataset":{"id":"…","path":"…","record_count":1000000,"bytes":268435456,"complete":false,"incomplete_reason":"max_bytes","offset":0,"returned":25,"ttl_seconds":3600,"read_hint":"…"}
```

Exit 0, with a `dataset_incomplete` warning. `complete:false` tells the agent this is not the whole
set.

### Upstream failed partway

```json
{"ok":false,"tool":"trello","command":"cards.list","data":["… 25 records …"],"error":{"code":"partial","exit_code":13,"message":"Collection stopped after 1800 of an expected 4213 cards: upstream returned 503.","retriable":true,"hint":"trello cards list --board 5f2a --all"},"meta":{"page":{"…":"…"},"dataset":{"id":"…","path":"…","record_count":1800,"complete":false,"incomplete_reason":"upstream_error","offset":0,"returned":25,"ttl_seconds":3600},"errors":[{"page":19,"code":"upstream","upstream_status":503}],"contract_version":"1.4","tool_version":"0.3.0"}}
```

The 1800 records collected are kept and readable.

## 5. Examples: one per error code

`meta` is shortened to `"…"`; it always carries at least `contract_version` and `tool_version`.

| Exit | Code | Example envelope |
|---|---|---|
| 1 | `internal` | `{"ok":false,"tool":"trello","command":"cards.list","data":null,"error":{"code":"internal","exit_code":1,"message":"The tool failed while encoding a card.","retriable":false,"hint":"Report this with --verbose output."},"meta":"…"}` |
| 2 | `usage` | `{"ok":false,"tool":"trello","command":null,"data":null,"error":{"code":"usage","exit_code":2,"message":"Unknown command: cards lst.","retriable":false,"hint":"trello tools","details":{"unknown_command":"cards lst","did_you_mean":"cards list"}},"meta":"…"}` |
| 3 | `config` | `{"ok":false,"tool":"trello","command":"cards.list","data":null,"error":{"code":"config","exit_code":3,"message":"The config file sets api_key to a value; credentials may only be referenced by path.","retriable":false,"hint":"Replace api_key with api_key_file=<path>.","details":{"file":"~/.config/agentcli/trello/config.json","member":"profiles.work.api_key"}},"meta":"…"}` |
| 4 | `auth` | `{"ok":false,"tool":"trello","command":"cards.list","data":null,"error":{"code":"auth","exit_code":4,"message":"No API key is configured.","retriable":false,"hint":"Set TRELLO_API_KEY, or TRELLO_API_KEY_FILE=<path>, or api_key_file=<path> in the config file."},"meta":"…"}` |
| 5 | `not_found` | `{"ok":false,"tool":"trello","command":"cards.get","data":null,"error":{"code":"not_found","exit_code":5,"message":"No card with id 91zz is visible to this token.","retriable":false,"details":{"upstream_status":404}},"meta":"…"}` |
| 6 | `validation` | `{"ok":false,"tool":"trello","command":"cards.create","data":null,"error":{"code":"validation","exit_code":6,"message":"Upstream rejected the card: due date is not a valid date.","retriable":false,"details":{"upstream_status":400,"upstream_excerpt":"invalid value for due"}},"meta":"…"}` |
| 7 | `conflict` | `{"ok":false,"tool":"trello","command":"cards.move","data":null,"error":{"code":"conflict","exit_code":7,"message":"The card changed since it was read.","retriable":false,"hint":"trello cards get 91bc","details":{"upstream_status":409}},"meta":"…"}` |
| 8 | `refused` | See §6 below |
| 9 | `rate_limited` | `{"ok":false,"tool":"trello","command":"cards.list","data":null,"error":{"code":"rate_limited","exit_code":9,"message":"Upstream is rate limiting this token.","retriable":true,"retry_after_ms":10000,"details":{"upstream_status":429}},"meta":"…"}` |
| 10 | `timeout` | `{"ok":false,"tool":"trello","command":"cards.list","data":null,"error":{"code":"timeout","exit_code":10,"message":"Upstream did not respond within 30s.","retriable":true,"hint":"trello cards list --board 5f2a --limit 10"},"meta":"…"}` |
| 11 | `network` | `{"ok":false,"tool":"trello","command":"cards.list","data":null,"error":{"code":"network","exit_code":11,"message":"Could not resolve api.trello.com.","retriable":true,"hint":"trello doctor"},"meta":"…"}` |
| 12 | `upstream` | `{"ok":false,"tool":"trello","command":"cards.list","data":null,"error":{"code":"upstream","exit_code":12,"message":"Upstream returned 502 after retries.","retriable":true,"details":{"upstream_status":502}},"meta":"…"}` |
| 13 | `partial` | See §4, "Upstream failed partway" |
| 14 | `cache_miss` | `{"ok":false,"tool":"trello","command":"dataset.read","data":null,"error":{"code":"cache_miss","exit_code":14,"message":"Dataset 3c9e2a4f expired; datasets live 1h after their last read.","retriable":false,"hint":"trello cards list --board 5f2a --all","details":{"id":"3c9e2a4f-7b1d-4e8a-9f60-2d5c8b1a7e34","reason":"expired"}},"meta":"…"}` |
| 130 | `canceled` | `{"ok":false,"tool":"trello","command":"cards.list","data":null,"error":{"code":"canceled","exit_code":130,"message":"Interrupted.","retriable":false},"meta":"…"}` |

Notes:

- The `cache_miss` hint can name the exact re-run because the sidecar records the original command
  and parameters.
- `validation` versus `usage`: `usage` means the tool rejected the command line before calling
  upstream; `validation` means upstream received the call and rejected its content.
- A tool-specific code (32–63) has the same shape, with `code` set to the name declared in
  `describe.exit_codes`.

## 6. Examples: writes

### Missing `--confirm`

```console
$ trello cards create --list-id 61a0 --name "Fix paging"
```

```json
{"ok":false,"tool":"trello","command":"cards.create","data":null,"error":{"code":"refused","exit_code":8,"message":"Writes require --confirm.","retriable":false,"hint":"trello cards create --list-id 61a0 --name \"Fix paging\" --confirm","details":{"reason":"confirmation_required","preview":{"method":"POST","url":"https://api.trello.com/1/cards","headers":{"Authorization":"***REDACTED***","Content-Type":"application/json"},"body":{"idList":"61a0","name":"Fix paging"}}}},"meta":"…"}
```

Nothing was sent. The preview shows exactly what would have been.

### Dry run

```json
{"ok":true,"tool":"trello","command":"cards.create","data":{"preview":{"method":"POST","url":"https://api.trello.com/1/cards","headers":{"Authorization":"***REDACTED***","Content-Type":"application/json"},"body":{"idList":"61a0","name":"Fix paging"}}},"meta":{"dry_run":true,"contract_version":"1.4","tool_version":"0.3.0"}}
```

### Credential on the command line

```json
{"ok":false,"tool":"trello","command":null,"data":null,"error":{"code":"refused","exit_code":8,"message":"A credential was passed on the command line and is now in your shell history and logs; rotate it.","retriable":false,"hint":"Set TRELLO_API_KEY_FILE=<path> instead.","details":{"reason":"secret_on_argv","flag":"--api-key"}},"meta":"…"}
```

The message never repeats the value.

### Write timed out

```json
{"ok":false,"tool":"trello","command":"cards.create","data":null,"error":{"code":"timeout","exit_code":10,"message":"No response within 30s; the card may or may not have been created.","retriable":false,"hint":"trello cards list --board 5f2a --since -10m","details":{"write_state":"unknown"}},"meta":"…"}
```

`retriable` is `false` here even though timeouts usually are: retrying a write blindly risks a
duplicate.

## 7. Diagnostics payloads

### `doctor` — healthy

```json
{"ok":true,"tool":"trello","command":"doctor","data":{"profile":"work","checks":[
  {"name":"config","status":"pass","detail":"Loaded ~/Library/Application Support/agentcli/trello/config.json; profile 'work' exists."},
  {"name":"credentials","status":"pass","detail":"API_KEY from TRELLO_API_KEY_WORK. API_TOKEN from ~/.secrets/trello.env (mode 0600)."},
  {"name":"network","status":"pass","detail":"api.trello.com resolved; TLS 1.3."},
  {"name":"auth","status":"pass","detail":"Authenticated as member 'guyr'."},
  {"name":"clock","status":"pass","detail":"Within 1s of upstream."},
  {"name":"datasets","status":"pass","detail":"~/Library/Caches/agentcli/trello/datasets is writable; 3 datasets, 1.4 MB."},
  {"name":"writes","status":"pass","detail":"Writes enabled in this build."}
]},"meta":{"contract_version":"1.4","tool_version":"0.3.0"}}
```

### `doctor` — failing

```json
{"ok":false,"tool":"trello","command":"doctor","data":{"profile":null,"checks":[
  {"name":"config","status":"pass","detail":"No config file; using environment and defaults."},
  {"name":"credentials","status":"fail","detail":"API_TOKEN is not set by any source."},
  {"name":"network","status":"pass","detail":"api.trello.com resolved; TLS 1.3."},
  {"name":"auth","status":"skip","detail":"Skipped: credentials incomplete."},
  {"name":"clock","status":"skip","detail":"Skipped: needs an authenticated response."},
  {"name":"datasets","status":"pass","detail":"Directory is writable."},
  {"name":"writes","status":"pass","detail":"Writes enabled in this build."}
]},"error":{"code":"auth","exit_code":4,"message":"A required credential is missing.","retriable":false,"hint":"Set TRELLO_API_TOKEN, or TRELLO_API_TOKEN_FILE=<path>, or api_token_file=<path> in the config file."},"meta":{"contract_version":"1.4","tool_version":"0.3.0"}}
```

`data` stays populated on failure, and the checks that could not run are `skip`, not missing.

### `list-config`

All seven precedence tiers in one response:

```json
{"ok":true,"tool":"trello","command":"list-config","data":{"profile":"work","config_file":"/Users/g/Library/Application Support/agentcli/trello/config.json","settings":[
  {"name":"TIMEOUT","value":"45s","source":"--timeout","origin":"flag","default":"30s"},
  {"name":"DATASET_TTL","value":"4h","source":"TRELLO_DATASET_TTL_WORK","origin":"env_profile","default":"1h"},
  {"name":"LIMIT","value":50,"source":"TRELLO_LIMIT","origin":"env_default","default":25},
  {"name":"BASE_URL","value":"https://api.trello.com","source":"config.json#profiles.work.base_url","origin":"file_profile","default":"https://api.trello.com"},
  {"name":"MAX_PAGES","value":20,"source":"config.json#default.max_pages","origin":"file_default","default":10},
  {"name":"LOG_LEVEL","value":"warn","source":"AGENTCLI_LOG_LEVEL","origin":"family","default":"error"},
  {"name":"MAX_BYTES","value":32768,"source":"builtin","origin":"builtin","default":32768},
  {"name":"API_KEY","value":null,"set":true,"source":"TRELLO_API_KEY_WORK","origin":"env_profile","default":null},
  {"name":"API_TOKEN","value":null,"set":true,"source":"~/.secrets/trello.env","origin":"file_default","default":null}
]},"meta":{"contract_version":"1.4","tool_version":"0.3.0"}}
```

Profile `work` is active but sets only `DATASET_TTL` and `BASE_URL`. `LIMIT` and `MAX_PAGES` keep the
values from lower tiers instead of reverting to defaults. That is per-setting inheritance.

For credentials, `source` names the variable or file, never the value.

### `list-config --schema`

```json
{"ok":true,"tool":"trello","command":"list-config","data":{"schema":{
  "$schema":"https://json-schema.org/draft/2020-12/schema",
  "type":"object","additionalProperties":false,
  "properties":{
    "config_version":{"const":1},
    "default":{"$ref":"#/$defs/settings"},
    "profiles":{"type":"object","propertyNames":{"pattern":"^[A-Za-z0-9][A-Za-z0-9_-]*$","not":{"enum":["FILE","file"]}},
                "additionalProperties":{"$ref":"#/$defs/settings"}}
  },
  "$defs":{"settings":{"type":"object","additionalProperties":false,"properties":{
    "limit":{"type":"integer","minimum":1},
    "timeout":{"type":"string","pattern":"^[0-9]+(ms|s|m|h)$"},
    "dataset_ttl":{"type":"string","pattern":"^[0-9]+(s|m|h|d)$"},
    "base_url":{"type":"string","format":"uri"},
    "api_key_file":{"type":"string","description":"Path to a file containing the API key. Never the key itself."},
    "api_token_file":{"type":"string","description":"Path to a file containing the token. Never the token itself."},
    "env_file":{"type":"string","description":"Path to a KEY=value file of credentials."}
  }}}
}},"meta":{"contract_version":"1.4","tool_version":"0.3.0"}}
```

There is no `api_key` property, only `api_key_file`. A file that validates cannot hold a credential.

## 8. Cursor format

A cursor is URL-safe base64, without padding, of a compact JSON object. The agent treats it as
opaque. It is documented here so every implementation produces compatible cursors.

```json
{"v":1,"c":"cards.list","u":"61","q":{"board":"5f2a","fields":"id,name,due","limit":60}}
{"v":1,"c":"dataset.read","d":"3c9e2a4f-7b1d-4e8a-9f60-2d5c8b1a7e34","o":125,"q":{"limit":100}}
```

| Key | Meaning |
|---|---|
| `v` | Format version. Anything other than `1` is `usage` |
| `c` | The command that accepts this cursor. A mismatch is `usage` |
| `u` | Upstream continuation: its own cursor, offset, page number, or next-page URL. Direct listing only |
| `d`, `o` | Dataset id and record offset. `dataset.read` only |
| `q` | The resolved parameters of the original call, so the cursor alone can continue it |
| `w` | The resolved time window, when the command has one. A resumed call reports `window.source:"cursor"` |

Rules:

- A cursor that cannot be decoded is `usage`. One that decodes but names a missing dataset is
  `cache_miss`. The first is a caller mistake; the second is a lifetime outcome.
- Cursors are not signed. They cross no trust boundary: a forged cursor reaches nothing a normal
  call could not.
- `q` holds parameters only, never credentials.

## 9. Warning codes

Warnings are an open set. These have fixed meanings:

| Code | Meaning |
|---|---|
| `limit_clamped` | `--limit` exceeded the maximum and was reduced |
| `dataset_incomplete` | A collection cap stopped `--all` early |
| `cleanup_failed` | Dataset cleanup could not run; the command itself succeeded |
| `permissions_unverified` | A credential file's permissions could not be checked on this platform |
| `flag_deprecated` | A flag is deprecated; the message names its replacement |
