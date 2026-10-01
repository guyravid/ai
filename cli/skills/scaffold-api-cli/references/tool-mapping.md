# Mapping an API to Commands

Used in Steps 3 and 6 of the skill.

## Naming

Dotted, `<group>.<verb>`. The command line is the name split on dots.

| Operation | Name | Command line |
|---|---|---|
| `GET /boards` | `boards.list` | `trello boards list` |
| `GET /boards/{id}` | `boards.get` | `trello boards get <id>` |
| `GET /boards/{id}/cards` | `cards.list` | `trello cards list --board <id>` |
| `GET /boards/{id}/actions` | `actions.list` | `trello actions list --board <id> --since -2h` |
| `POST /cards` | `cards.create` | `trello cards create … --confirm` |
| `PUT /cards/{id}` with `idList` | `cards.move` | `trello cards move <id> --list-id <id> --confirm` |
| `DELETE /cards/{id}` | `cards.delete` | `trello cards delete <id> --confirm` |

- **The group is the thing returned, not the first path segment.** `GET /boards/{id}/cards` returns
  cards, so it is `cards.list`, and all card commands sort together in `tools`.
- Verbs: `list` for collections, `get` for one by id, `search` for a query, `count` where the API can
  count cheaply. Writes: `create`, `update`, `delete`, or a specific action such as `move`.
- **Anything that changes upstream is declared mutating** and gated by `--confirm`. Check every
  POST, PUT, PATCH, and DELETE; and check GETs too, since some APIs perform actions on GET. A `write`
  name prefix (`write.cards.create`) is optional; if the tool uses it, only mutating commands carry it.
- Segments are lowercase letters, digits, and hyphens. No group may be a reserved name: `tools`,
  `describe`, `teach`, `doctor`, `list-config`, `dataset`, `serve`, `version`, `write` (other than as
  the optional prefix of a mutating command), `help`.

## From an OpenAPI operation

| OpenAPI | Command definition |
|---|---|
| Path and method | `name`, `argv` |
| `summary` | `description`, rewritten to one line of at most 160 characters |
| Path parameter for the main id | A positional argument |
| Other parameters and body fields | Flags, via the input type |
| `required` | Required parameters |
| Response schema | The output type; `outputSchema` is derived from it |
| Response is an array | `paged`, and usually `collectable` |
| A time-based filter exists (`since`, `before`, `after`) | `time_window`, mapped to `--since` / `--until` |
| Method is not GET/HEAD | Mutating: `mutates:true`, `readOnlyHint:false`, `destructiveHint` for deletions |

Rewrite descriptions rather than truncating them. An OpenAPI description cut at 160 characters
usually ends mid-sentence and makes `tools --detail` look broken.

Derive names from path and method. `operationId` is often machine-generated and unusable.

## Fields

For each list command:

- `fields.available`: every field seen in the documentation or a real response, as dotted paths for
  nested objects (`badges.comments`).
- `fields.default`: the few an agent needs to decide what to look at next. Aim for four to six:
  usually an id, a name or title, a state, and a timestamp. Descriptions, full member lists, and
  embedded objects belong in `get`, not in the default list projection.

For `get` commands, the default can be broader, but still drop bulky fields that are rarely needed.

## Sort

Every list command declares its order, ending with a unique tiebreak:

```
dateLastActivity desc, id asc
```

If the API does not guarantee an order, sort locally after fetching. Relying on upstream order makes
two identical calls return different bytes.

## Pagination styles

| Style | Recognised by | Stored in the cursor | Exhausted when |
|---|---|---|---|
| Opaque cursor | Response has `next`, `next_cursor`, `after`, `page_token` | The token | The token is absent, null, or empty. A short page does not mean the end |
| Offset | Request takes `offset` or `skip` | The next offset | A short page, or `total` reached |
| Page number | Request takes `page` | The next page number | A short page, or `total_pages` reached |
| Link header | `Link: <…>; rel="next"` | The next URL | No `next` relation. Parse the header properly; URLs can contain commas |
| Id-based | Request takes `before=<id>` (Trello does this) | The last id seen | A short page |
| None | Everything in one response | A local offset | The local list is consumed |

Record the API's maximum page size. `--all` uses it regardless of `--limit`.

## Status codes to error codes

| Upstream | Code | Exit | Note |
|---|---|---|---|
| 400, 422 | `validation` | 6 | Upstream rejected the content. Not `usage`: the command line was fine |
| 401 | `auth` | 4 | |
| 403 | `auth` | 4 | Hint should mention permissions or scope, not a missing key |
| 404 on the addressed resource | `not_found` | 5 | |
| 404 on the API root | `upstream` | 12 | A wrong base URL, not a missing record |
| 405, 501 | `internal` | 1 | The tool built a request the API does not support |
| 408, 504 | `timeout` | 10 | |
| 409, 412 | `conflict` | 7 | |
| 429 | `rate_limited` | 9 | Pass `Retry-After` into `retry_after_ms` |
| 500, 502, 503 | `upstream` | 12 | After the tool's retries |
| 200 with an error body | From the body | | Some APIs never use status codes; map the body's own error field |
| DNS, connection, TLS failure | `network` | 11 | |
| Tool's own deadline | `timeout` | 10 | For a write: `details.write_state:"unknown"`, `retriable:false` |

API-specific conditions that deserve their own code (an archived board that rejects changes, say)
get a tool-specific code between 32 and 63, declared in `describe`.

## Input types

- One field per parameter, named as the API names it unless that name is unusable.
- Paging, time, field, and size flags come from the substrate. Do not declare them per command.
- Required parameters have no `omitempty`; optional ones do.
- Declare the default and maximum `--limit` in the command's `limits`.
- Keep types concrete. A free-form `map` parameter produces a schema an agent cannot use.

## Output types

`outputSchema` describes `data` only. Prefer a concrete type listing the fields you know about. It
gives agents a real schema, and it is the only way to leave out fields that should never be shown or
stored. Where the shape is genuinely unknown, use raw JSON with a permissive schema, and report it as
a TODO.
