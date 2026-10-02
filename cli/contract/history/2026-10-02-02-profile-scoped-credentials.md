---
date: 2026-10-02
contract_version: "1.4.2"
change: patch
supersedes: null
---
# 1.4.2: optional profile-scoped credentials

## Summary

A tool MAY declare a credential **profile-scoped**. When a profile is selected, such a credential
resolves only from the profile scope (the suffixed variables, `profiles.<p>.*`, and a profile-keyed
line in the `default` section's `env_file`), never from the default scope (the unsuffixed variables and
the `default` section). It is for tools whose profiles stand for separate identities, such as one bot
or one account per profile, where falling back to the default identity would silently act as the wrong
one.

## Changes

- §12.1: names the two scopes (steps 3, 4, 6 are the default scope; steps 1, 2, 5 and a profile-suffixed
  `default.env_file` key are the profile scope) and adds the optional rule. For a profile-scoped
  credential with a profile selected, only the profile scope supplies it. If it supplies nothing and the
  default scope would have, the result is `config` (exit 3) with `details.profile` and
  `details.would_use_source` (never a value); if neither scope has one, the usual `auth`. With no profile
  selected, resolution is unchanged. Settings still inherit (§13.2).
- §6.2: `auth.credentials[]` entries gain an optional `profile_scoped:true`, omitted by a tool that does
  not use the feature.
- §7.2: for a profile-scoped credential, `list-config` reports the profile-scope source actually used;
  when only the default scope has one, the entry is `set:false` with a `hint` naming the skipped source
  and the profile-scoped ways to supply it.
- §7.1: the `credentials` check reports the profile-scope source used, or fails with exit 3 as above.
- §6.3: in a tool with any profile-scoped credential, the `teach config` credentials aspect MUST say
  which credentials are profile-scoped and what that means.
- Header: `1.4.2`.

## Rationale

Plain §12.1 lets the default scope take part in every resolution, and the unsuffixed variables outrank
the profile's own section of the config file. For a tool where a profile is an identity, a profile
without its own credential, or a stray exported `<TOOL>_<NAME>`, then sends as the default identity
without anyone noticing. Making the narrowing a declared, optional behaviour keeps it visible to agents
(`describe`, `list-config`, `doctor`, `teach config`) and testable (the conformance checks run only when
`describe` marks a credential profile-scoped).

It is optional, so it is a patch under §18: nothing is added that an existing tool fails, and a tool that
does not declare a profile-scoped credential resolves exactly as before. Tools whose profiles are only
different views of one identity, such as trello, keep the shared fallback.

## Migration

- Tools: nothing is required. A tool conforming to 1.4.1 conforms to 1.4.2 unchanged.
  - To opt in, resolve the credential from the profile scope only when a profile is selected (do not
    resolve normally and check the winner afterwards), report the `config` and `auth` outcomes as §12.1
    says, mark the credential `profile_scoped:true` in `describe`, cover the `teach config` credentials
    aspect, and report `1.4.2`.
  - New tools whose profiles are separate identities should opt in.
- Configurations: with a profile-scoped credential, a profile needs its own source; it no longer
  inherits the default one. Add `profiles.<p>.<name>_file` or `<TOOL>_<NAME>_FILE_<PROFILE>`.
- Callers: a `config` error with `details.would_use_source` means the profile lacks its own credential;
  run the hint, or `doctor`.
