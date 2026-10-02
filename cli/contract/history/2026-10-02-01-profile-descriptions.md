---
date: 2026-10-02
contract_version: "1.4.1"
change: patch
supersedes: null
---
# 1.4.1: optional profile descriptions, and a patch level

## Summary

The contract version gains a patch component, `major.minor.patch`, for changes that add only optional
behaviour. The first such change: a tool MAY support a one-line `description` for `default` and for
each profile in its config file, and a tool that does reports it on every `list-profiles` entry, so an
agent can choose a `--profile` by what it is for rather than by its name.

## Changes

- §18: the version is `major.minor.patch`, and the patch may be omitted when it is zero (`1.4` and
  `1.4.0` are the same version). A change that adds only optional behaviour (MAY) bumps the patch,
  provided every tool conforming to the previous version still conforms unchanged: a patch change never
  changes an exit code, a flag's meaning or the envelope's shape, and never adds a requirement an
  existing tool fails. A tool reports the exact version it implements in `meta.contract_version`.
  A patch change also never narrows what a caller must accept; this version, which introduces the
  patch level, is the one exception (see Migration).
- §13.4: a tool MAY accept `description` in `default` and in each profile (rule 6). A tool that does
  validates it: a string, one line, at most 200 characters, not empty after trimming; a violation is
  `config`, with a hint naming the member by pointer. It is not a setting: it has no tier in §13.2, no
  variable, no flag, and no row in `list-config`. Rule 1 now reads "Member names are lowercase setting names, apart from `description` (rule 6)." Rule 2 notes that a supporting tool treats it as
  recognised; a tool that does not support it still rejects it as unrecognised. The example file gains
  descriptions.
- §7.3: a tool that supports descriptions MUST put `description` on every `list-profiles` entry: the
  file's value, or `null` when unset, including for `default` and for profiles declared only by
  variables. It is never empty-stripped (§8.3 does not apply). A tool that does not support
  descriptions omits the member. Rule 4 now reads "names and descriptions only". The example carries
  `description` and `contract_version` `1.4.1`.
- §6.3: in a tool that supports descriptions, the `teach config` profiles aspect MUST explain
  `description`.
- Header: `1.4.1`. The other examples keep `contract_version` `1.4`, which is still a valid
  version.

## Rationale

A profile name such as `work` or `bot2` rarely says which profile fits a task, so an agent either
guesses or asks. A sentence written by the operator answers it at the point of choice, in the command
that already exists for choosing. Capping it at 200 characters on one line keeps `list-profiles`
cheap and untruncatable (A1).

The behaviour is optional because requiring it would make every existing tool non-conforming, which
is a minor change at least. Optional support can be added tool by tool, and the contract's existing
version scheme had no way to say "added, but nobody has to follow". The patch level says exactly
that. Both sides of the rule are explicit: a tool that does not support descriptions omits the
member and keeps rejecting it in config, and an agent treats a missing description and a `null` one
the same way.

## Migration

- Tools: nothing is required. A tool conforming to 1.4 conforms to 1.4.1 unchanged and may keep
  reporting `1.4`.
  - To support descriptions, follow §13.4 rule 6 and §7.3 exactly: validate the member, emit it on
    every entry, keep it out of `list-config` and the settings resolver, mention it in the `teach
    config` profiles aspect, and report `1.4.1`.
  - New tools should support them.
- Configurations: do not add `description` to a config file read by a tool that does not support it.
  That tool rejects it as an unrecognised member (`config`, exit 3).
- Callers: treat a missing `description` and a `null` one the same way.
- Callers: accept a three-component `contract_version` (`1.4.1`) in `meta` and in `version`. The
  patch guarantee covers tools, not callers. A caller that validates envelopes against the 1.4
  `envelope_schema` (pattern `^\d+\.\d+$`), or parses the version as exactly two numbers, rejects
  `1.4.1`. Validate against the schema the tool itself publishes in `describe`, which carries the
  widened pattern `^\d+\.\d+(\.\d+)?$`, or compare versions component by component, treating a
  missing patch as `0`.
