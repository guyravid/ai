---
summary: One profile is one bot: choosing it, what it inherits, and the token isolation rule
---
## One profile, one bot

A profile is a bot: its token plus its chat settings. Running without `--profile` uses the `default`
section, which is the PR-assistant bot. Other bots are named profiles. `{{tool}} list-profiles` lists
them with a one-line `description` of what each is for (null when the operator wrote none), so pick
the profile from the descriptions and pass `--profile <name>`. `{{tool}} doctor --profile <name>`
shows which bot a profile reaches, by username.

## A profile never borrows the default bot's token

A selected profile resolves its bot token only from its own scope, never from the default scope.
The profile scope, in this order:

1. `TELEGRAM_BOT_TOKEN_<PROFILE>`
2. `TELEGRAM_BOT_TOKEN_FILE_<PROFILE>`
3. the profile's own `bot_token_file` (or its `env_file` holding the key)
4. a line keyed `TELEGRAM_BOT_TOKEN_<PROFILE>` in the default section's `env_file`

The default scope is skipped entirely for a selected profile: `TELEGRAM_BOT_TOKEN`,
`TELEGRAM_BOT_TOKEN_FILE`, the default section's `bot_token_file`, and an unsuffixed key in its
`env_file`. A profile with its own `bot_token_file` therefore uses it even when
`TELEGRAM_BOT_TOKEN_FILE` is set. If a profile has nothing in its own scope but the default scope has
a token, the call fails with `config` (exit 3), because a typo or a missing token would otherwise
send as the wrong bot; the error names the profile and the skipped source, never a value. With no
token anywhere it is the ordinary `auth` error. `list-config` reports the source actually used, or
`set: false` with a hint naming the skipped one, and `doctor` fails its `credentials` check.

## What a profile does inherit

Every other setting still inherits from `default` when the profile leaves it out, including
`default_chat` and `allowed_chats`. A profile that sets its own `bot_token_file` but not
`default_chat` therefore addresses the same chats as the default bot. That is intended: a private
chat id is the person's user id, the same for every bot, but only after that person has pressed
Start on each bot. Set `default_chat` and `allowed_chats` in the profile when its bot should reach
other chats.
