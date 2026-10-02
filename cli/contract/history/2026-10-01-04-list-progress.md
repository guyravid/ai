---
date: 2026-10-01
contract_version: "1.3"
change: minor
supersedes: null
---
# 1.3: A list response that has records always carries one

## Summary

§8.4 now says what a list response holds when not even one record fits under `--max-bytes`. The
first record is kept with its largest strings shortened, so the response always carries at least
one record and `next_cursor` always moves forward.

## Changes

- §8.4: when not even one whole record fits, the tool MUST keep the first and shorten its largest
  strings, as for a single object, listing the shortened paths in `meta.elided_fields`.
- §8.4: if that record still does not fit, the cap is ignored for the response, which carries the
  one record, with `truncated_reason:"max_bytes_below_minimum"`; `next_cursor` points past it.
- §8.4: a response with no records whose envelope alone exceeds the cap is sent whole with the same
  reason. Before, that was the only below-minimum case the contract named.
- Example envelopes carry `contract_version` `1.3`.

## Rationale

The old text dropped whole records "until it fits" and ignored the cap only for "the envelope
alone". Read literally, a list whose first record exceeds the cap returned zero records, with
`next_cursor` pointing at that same record. A caller following the cursor, as §9.3 tells it to, then
repeated the same request forever: the most damaging failure a paging contract can have, and silent.

Keeping one shortened record mirrors the rule §8.4 already gives single objects, and guarantees
progress. `trello`, the first tool built from the contract, already behaved this way. Making it a
rule turns a tool-level choice into one every caller can rely on.

This is `minor`, not `editorial`: a tool that returned zero records conformed to 1.2 and does not
conform to 1.3.

## Migration

- Tools: when not even one record fits, keep the first and shorten its strings instead of returning
  an empty page. Shorten from the original text each time, halving the length kept, so that each
  step strictly reduces one string and the loop always ends. Shortening the already-marked text
  stacks markers, and once a string is short, half of it plus the marker is no shorter, so the loop
  can spin forever.
- Callers: none. A page may now be smaller than the cap allows, but it is never empty while records
  remain.
- `contract_version` becomes `1.3`.
