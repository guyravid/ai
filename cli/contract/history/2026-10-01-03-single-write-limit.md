---
date: 2026-10-01
contract_version: "1.2"
change: editorial
supersedes: null
---
# 1.2: The single-write guarantee states its limit

## Summary

§2 rule 2 still requires one write per document. Its rationale claimed that an interrupted tool
always leaves nothing or a complete document. That holds for files and for documents that fit in a
pipe's buffer, but not for larger documents written into a pipe. The rationale now says where the
guarantee stops. No rule changed, so the version stays 1.2.

## Changes

- §2 rule 2, *Why*:
  - Through a pipe, the operating system accepts only what fits in its buffer (64 KiB on Linux and
    macOS) and blocks the rest of the write.
  - A kill that cannot be caught, arriving while a larger document is blocked, cuts the document off.
  - The default `--max-bytes` fits within that buffer.
  - A caller that raises the cap should treat invalid JSON together with a non-zero exit as an
    interrupted call.
  - SIGINT and SIGTERM let a write already under way complete.
- Aligned outside the contract:
  - `base/patterns/go.md` §4 notes the limit, and says not to work around it.
  - Checklist C2-6 now tests the guarantee where it holds.

## Rationale

Testing C2-6 against `trello` showed:

| Situation | Result |
|---|---|
| Stdout redirected to a file, SIGKILL at random points (300 runs) | 73 empty, 227 complete, none partial |
| SIGTERM or SIGINT during a blocked pipe write | the complete document was delivered |
| A 1 MiB document into a stalled pipe, SIGKILL while blocked | the reader received exactly 65,536 bytes of broken JSON, every time |

The tool performed one write call in every case. The cut comes from how pipes work, not from the
tool, so no tool-side rule can remove it. Holding every document under the pipe buffer would mean
lowering the `--max-bytes` maximum. That is a change to a flag's meaning and a major version, and it
was not made. Even then, POSIX promises atomic pipe writes only up to 512 bytes.

## Migration

None for tools: the rule is unchanged. A caller that raises `--max-bytes` above 64 KiB and reads
through a pipe should already be checking that output parses. A non-zero exit with unparseable
output is an interrupted call, not a tool defect.
