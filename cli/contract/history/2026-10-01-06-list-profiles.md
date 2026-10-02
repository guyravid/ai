---
date: 2026-10-01
contract_version: "1.4"
change: minor
supersedes: null
---
# 1.4: `list-profiles`

## Summary

A new built-in command, `list-profiles`, lists the profiles a tool can run with. When no profile is
declared it returns an empty list. Otherwise it returns `default`, which stands for running without
`--profile`, followed by every declared profile, with where each is declared and which one is
active. A profile may no longer be named `default`.

## Changes

- §6: `list-profiles` joins the commands that must work with nothing configured.
- §6.3: bare `teach` names `list-profiles` among the commands the contract provides.
- §7.3 (new): `list-profiles`, with its output and rules:
  - `data` is `[]` when no profile is declared;
  - otherwise `default` comes first, then profiles sorted by name;
  - each entry has `{name, active, declared_in}`;
  - a profile declared only by environment variables is listed under its variable segment, in
    lowercase;
  - names only, never values;
  - an undeclared selected profile gives a warning, not a failure.
- §13.3: a profile MUST NOT be named `default` (matched without regard to case, like `FILE`). The
  `config` error for an undeclared profile SHOULD hint at `list-profiles`.
- §16.2: `list-profiles` is not offered over MCP, because a server's profile is fixed at startup.
- §17: `list-profiles` is a reserved command name.
- Example envelopes carry `contract_version` `1.4`.

## Rationale

Before 1.4, nothing told an agent which profiles existed. `list-config` reports only the active
profile, and a mistyped one fails with `config` without naming the alternatives. Finding them meant
reading the config file and scanning the environment: the work these tools exist to spare an agent,
and close to the credentials it should never handle.

A dedicated command keeps the answer to "what can this tool run as?" separate from "what is it
configured as?". Listing `default` beside the declared profiles makes running without `--profile`
visibly one of the choices. Returning `[]` when nothing is declared says, with no ambiguity, that
there is nothing to choose.

Environment variables carry only the uppercased form of a profile name, with `-` written as `_`, so
a profile declared only there cannot be shown under its original spelling. Its segment in lowercase
is a name that selects it, which is what the list is for.

## Migration

- Tools:
  - add `list-profiles` per §7.3, and name it in bare `teach`;
  - reject `default` as a profile name, in the config file, in variables, and in `--profile`;
  - point the undeclared-profile hint at `list-profiles`;
  - leave `list-profiles` out of the MCP tool list.

  Finding profiles declared in the environment needs a way to enumerate the variables, not only to
  look one up by name.
- Configurations: a profile named `default` becomes a `config` error. Rename it.
- `contract_version` becomes `1.4`.
