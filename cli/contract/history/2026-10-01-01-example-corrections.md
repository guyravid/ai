---
date: 2026-10-01
contract_version: "1.1"
change: editorial
supersedes: null
---
# 1.1: Examples corrected to follow the contract's own rules

## Summary

Three examples in the contract broke rules the contract itself sets: one hint used a dotted command
name as a command line, and two examples gave a list parameter the reserved flag `--list`. The
examples now follow the rules. No normative text changed, so the version stays 1.1.

These corrections shipped in the same commit as 1.1 (`733eee1`) without an entry of their own; this
file records them.

## Changes

- §3.3: the failure example's hint `trello cards.list --board <board-id> --limit 20` is now
  `trello cards list --board <board-id> --limit 20`. Command lines are space-separated (§5); dotted
  names identify commands in envelopes and discovery only.
- §6.2: the `describe` example's list parameter is `list_id` with flag `--list-id`, in both
  `inputSchema.properties` and `x-cli.flags`. It was `list` with `--list`. The output field `list` in
  `fields` is unchanged; it is a record field, not a parameter.
- §11.1: the write example is `trello cards create --list-id 61a0 --name "Fix paging" --confirm`.
- Aligned outside the contract: `base/patterns/envelope.md` §6 ("Missing `--confirm`"), in both the
  command line and the refusal's `hint`.

## Rationale

`--list` is reserved for `teach` (§6.3, §17), and a tool MUST NOT use a reserved flag for anything
else. Examples are copied more often than rules are read, so an example that breaks a rule spreads
the defect into every tool scaffolded from it. The first tool built from the contract (`trello`) hit
exactly this, and named its parameter `--list-id`.

## Migration

None for conforming tools: no rule changed. A tool that copied the old examples and offers `--list`
as a parameter flag was already non-conforming; rename the parameter (for example to `--list-id`)
and its MCP argument to match.
