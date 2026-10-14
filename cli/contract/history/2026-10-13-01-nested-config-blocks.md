---
date: 2026-10-13
contract_version: "1.4.3"
change: patch
supersedes: null
---
# 1.4.3: optional nested blocks in the config file

## Summary

A tool MAY accept nested JSON objects ("blocks") in the config file's `default` section and in each
profile, as file syntax for flat settings. A path joins with `_` and uppercases to the setting's
name, so `telemetry.otlp.endpoint` is `TELEMETRY_OTLP_ENDPOINT`, read from
`<TOOL>_TELEMETRY_OTLP_ENDPOINT` and `--telemetry-otlp-endpoint`. Environment variables, flags, and
`list-config` names stay flat. Each setting has one spelling in the file, fixed by the tool's schema.
A tool that does not support blocks rejects them as before, so every existing tool conforms
unchanged.

## Changes

- §13.4: rule 7 (new) lets a tool accept nested blocks. A new subsection, "Nested blocks (optional,
  since 1.4.3)", gives an example file and seven rules for a tool that does:
  1. a path joined with `_` and uppercased is the setting's name, and its variables and flag follow
     from that name; variables and flags are never nested;
  2. no two paths flatten to the same name, including the name of a flat member, `<name>_file`, or
     `env_file`; a member is either a block or a value;
  3. one spelling per setting, fixed by the schema; the other spelling is an unrecognised member;
  4. profiles merge setting by setting, never block by block; an empty block changes nothing;
  5. an unknown member at any level, or a block holding something other than an object, is
     `config`, and the error names the full path;
  6. credentials stay paths inside blocks: only `<name>_file` at the credential's path; a value
     there is `config` and is never echoed; `env_file` belongs to the section, never to a block, and
     its keys stay flat;
  7. `list-config` reports the flat name, with `source` pointing at the member's path
     (`config.json#profiles.work.telemetry.otlp.endpoint`); `list-config --schema` shows each block
     as a nested object with `additionalProperties:false`.

  A closing paragraph states that a tool without support rejects a block under rule 2 (or as an
  invalid value) and needs no change. Rule 1 now allows a path's member names, rule 2 applies at
  every level in a supporting tool, and the member table says `default` and `profiles` may hold
  blocks.
- §13.2: in a supporting tool, `<setting>` in tiers 4 and 5 is the member path; every other tier uses
  the flat name.
- §7.2: a `source` pointer names the member at its path. In a supporting tool, `list-config --schema`
  shows the nesting while `settings[].name` stays flat.
- §6.3: in a supporting tool, `teach config` lists every setting with its member path as well as its
  flat name, and its example file uses the nesting.
- Header: `1.4.3`. The other examples keep their `contract_version`, which is still valid.
- Aligned outside the contract: the conformance checklist and verifier (C13-20 for every tool; C13-21
  to C13-26 and C7-17, run only when `list-config --schema` declares a nested setting),
  `base/BASE_TEMPLATE.md` §5.2 and §5.3, `base/patterns/go.md` §5, and `AGENT.md` (configuration).
  The shared `teach` templates are unchanged: nothing in them describes the file's syntax.

## Rationale

Putting a group of related settings, such as telemetry, into one block of `config.json` broke rule 1
of §13.4, which required flat lowercase setting names because a setting is one name across every
precedence tier, environment variables and flags included. The alternative, flat `telemetry_otlp_*`
members, works but reads poorly once a tool has several such groups.

The amendment keeps the one-name property instead of relaxing it: a block is only a way of writing
the flat name, and everything outside the file (variables, flags, `list-config`, `describe`) keeps
using it. The joining rule is mechanical, so an operator can derive the variable from the path and
back; forbidding collisions keeps that derivation unambiguous and the setting names a closed set
(§13.3). Merging per setting carries §13.2's "a profile overrides only what it defines" into blocks,
where block-by-block replacement would be the natural bug. Requiring one spelling per setting was
chosen over accepting both spellings with the nested one winning: two spellings need a precedence
rule within a section and across sections, and give `list-config` two possible sources for one
setting, while one spelling has nothing to decide and the schema states it.

It is optional, so it is a patch under §18: no exit code, flag, or envelope shape changes, and
nothing is added that an existing tool fails. A tool that does not support blocks already rejected
them under rule 2, and still does.

## Migration

- Tools: nothing is required. A tool conforming to 1.4.2 conforms to 1.4.3 unchanged and may keep
  reporting `1.4.2`.
  - To adopt it: declare a path for each nested setting (and for each nested credential) whose
    segments join with `_` to the setting's name; reject at build or start-up any declaration where
    two paths flatten to one name or a member is both a block and a value; flatten each section to
    flat names while loading, rejecting unknown members and non-object blocks at every level and
    naming the full path; resolve per flat name, so profiles merge per setting; keep `env_file` at the
    section level; point `list-config` sources at the path; nest the `list-config --schema`
    properties; list the member path in `teach config` and nest its example; and report `1.4.3`.
  - The Go substrate in `tools/telegram/internal/config` implements this behind `Def.FilePath` and
    `Input.CredentialPaths`. The telegram tool declares no nested settings, so its behaviour is
    unchanged.
- Configurations: do not use blocks with a tool that does not declare them in `list-config
  --schema`; it rejects them (`config`, exit 3). With a tool that does, write each setting at its one
  declared spelling.
- Callers: nothing. Setting names, variables, and flags are flat as before; only `source` pointers
  can be longer.
