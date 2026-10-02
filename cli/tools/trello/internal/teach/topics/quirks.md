---
summary: Rate limits, paging behaviour, and other things that surprise first-time callers
---
## Rate limits

Trello allows 300 requests per 10 seconds per API key and 100 per 10 seconds per token. A throttled
call returns `rate_limited` (exit 9). Prefer `--all` over many small pages when you need everything.

## Lists that do not page upstream

Boards, lists, cards on a board, and search results come back from Trello in one response. The tool
fetches the whole set and pages it for you, so `total` is exact. Each call, including one continued
with `--cursor`, fetches the set again; use `--all` and `dataset read` to avoid that.

## Archived things

`boards list` and `lists list` show open items unless you pass `--filter closed` or `--filter all`.

## Card descriptions

`cards list` leaves `desc` out by default because descriptions can be long. Use `cards get <id>`, or
add it with `--fields id,name,desc`.
