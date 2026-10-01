---
date: 2026-09-30
contract_version: "1.1"
change: minor
supersedes: null
---
# 1.1 — Writes are declared, the `write` prefix is optional

## Summary

A command that changes upstream state is now identified by its declaration in discovery
(`mutates:true`, `readOnlyHint:false`) and gated by `--confirm`, not by a mandatory `write` prefix in
its name. The prefix remains allowed; a name that uses it must belong to a mutating command.

## Changes

- §1 A5 rewritten: "Writes are declared and confirmed."
- §5 rule 2: mutating commands MUST be declared; a name MAY start with `write`, and if it does the
  command MUST be mutating. Naming example `write.cards.create` → `cards.create`.
- §6.1: `mutates` is true exactly when the command changes upstream state.
- §6.2: `readOnlyHint` is true exactly when the command is not mutating.
- §10.3: dataset commands are described as not mutating, instead of as not using the prefix.
- §11.1 retitled "Declaring writes", with the new rule, its rationale, and its cost.
- §11.2: the `--confirm` requirement applies to mutating commands.
- §11.3: read-only builds hide mutating commands and refuse a command line naming one with
  `writes_disabled`, recognising mutating names for that purpose.
- §16.2: wording follows mutating commands rather than `write.*` names.
- §17: `write` stays reserved as a group name, usable only as the optional first segment of a
  mutating command.
- Example envelopes carry `contract_version` `1.1`.

## Rationale

The prefix made command lines awkward (`trello write cards create`) while `--confirm` already gated
every write and discovery already carried the information. Declaring writes puts the information
where agents and MCP clients read it.

The cost is explicit: a permission rule that matches only the start of a command line can no longer
separate reads from writes when a tool drops the prefix. The read-only build is the hard separation,
as it already was for MCP clients, or operators deny each mutating command by name.

This is a minor version because it only relaxes a rule: every 1.0 tool, whose mutating commands all
start with `write`, still conforms to 1.1 unchanged. No exit code, flag, or envelope shape changed.

## Migration

- Existing 1.0 tools: no change required. Report `contract_version` `1.1` when next released.
- New tools may name mutating commands like any other (`cards.create`), declare them mutating, and
  keep the `write` prefix only if they want prefix-based permission rules to work.
- Operators who relied on a `write *` deny rule should, for tools without the prefix, use the
  read-only build or deny mutating commands by name; `tools --detail` lists which they are.
