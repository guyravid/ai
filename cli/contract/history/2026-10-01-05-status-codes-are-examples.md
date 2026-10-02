---
date: 2026-10-01
contract_version: "1.3"
change: editorial
supersedes: null
---
# 1.3: Status codes in the error table are examples

## Summary

§4 maps upstream outcomes to error codes and gives HTTP statuses in parentheses. Those statuses read
as a closed list, which sent a 413 "File too large" to `upstream`, a retriable code, although the
definition of `validation` ("upstream rejected its content") plainly covers it. The table now says
its statuses are examples, and `validation` lists 413. The version stays 1.3.

## Changes

- §4, `validation` row: "(400, 422)" becomes "(such as 400, 413, 422)".
- §4, after the table: status codes are examples. The error code follows from what upstream did, so
  a response rejecting the request's content is `validation` whatever its status.
- Aligned outside the contract:
  - `base/BASE_TEMPLATE.md` §3 mapping table: 400 / 413 / 422.
  - `skills/scaffold-api-cli/references/tool-mapping.md` status table: 413 added, with a note that
    the code is not retriable.

## Rationale

A live upload of 10.1 MiB to Trello was answered with 413 "File too large". `trello` mapped it to
`upstream`, exit 12, `retriable:true`, which tells an agent to try again: the request can never
succeed. The meanings in §4 were always the rule and the numbers were illustrations. Reading them
as a closed list produced the wrong code, so this makes the text say what it meant. A tool that
followed the definitions already mapped 413 this way.

## Migration

Tools that map 413 to `upstream` should map it to `validation`. Callers need nothing: the code they
receive for an oversize request now matches its meaning, and they stop retrying it.
