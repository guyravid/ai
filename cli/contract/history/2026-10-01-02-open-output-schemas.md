---
date: 2026-10-01
contract_version: "1.2"
change: minor
supersedes: null
---
# 1.2: Output schemas allow every shaped response

## Summary

`outputSchema` now describes records before shaping, and may not forbid anything shaping produces:
no required properties, no closed objects, the depth-elision marker allowed below the record, and no
constraints that a shortened string would break. A command's full output schema admits `data:null`.
Each MCP tool publishes that full output schema.

## Changes

- §6.2: new rules for `outputSchema`, with their rationale:
  - no object schema lists `required` or sets `additionalProperties:false`;
  - every object or array schema nested inside a record also admits `"<depth-elided>"`;
  - no string schema sets `maxLength`, `pattern`, `enum`, or `const`.
- §6.2: a command's full output schema replaces `data` with
  `{"anyOf":[<outputSchema>,{"type":"null"}]}`, not with `outputSchema` alone.
- §16.2: each tool's `outputSchema` is the command's full output schema.
- Example envelopes carry `contract_version` `1.2`.

## Rationale

The first tool built from the contract derived its output schemas from typed structs. The derivation
marked every field without `omitempty` as required and closed every object. Every one of these
correct responses then broke its own schema:

- `--fields id,due` drops `name`;
- empty-stripping removes `name:""`;
- `--fields labels.name` drops each label's `id`;
- `--max-depth` replaces a nested object with a string.

On the command line nothing enforces `outputSchema`. Over MCP, a server that declares an output
schema must return results that conform, and clients are encouraged to check. A strict schema
therefore turns correct output into client-side failures.

The full output schema also contradicted §3.1: a failure carries `data:null`, which an object
`outputSchema` rejects.

The rules keep what agents use: field names, types, and descriptions. They drop only constraints the
shaping rules already make untrue.

## Migration

- Tools that derive schemas from types: post-process the derived `outputSchema` before publishing it
  in `describe` and over MCP:
  - remove `required`;
  - remove `additionalProperties:false`;
  - wrap each object or array schema below the record in `{"anyOf":[<schema>,{"const":"<depth-elided>"}]}`;
  - remove `maxLength`, `pattern`, `enum`, and `const` from string schemas.
- MCP servers: publish `data` as `anyOf` with `null`.
- `contract_version` becomes `1.2`.

No envelope, exit code, or flag changes, so callers need no migration.
