# How it works

## Files

```text
~/.codexctl/
├── current        # name of the active profile
├── lock           # prevents two codexctl commands running at once
├── switched       # when the last switch happened (no credentials)
└── profiles/
    ├── personal.json
    └── work.json
```

Set `CODEXCTL_HOME` to use another directory. Codex's own files live in
`CODEX_HOME` (default `~/.codex`).

## Switching

`codexctl use NAME`:

1. Saves any tokens Codex refreshed for the current profile.
2. Copies the new profile to `$CODEX_HOME/auth.json` in one atomic write.
3. Records the new selection.

If codexctl is interrupted halfway, the next `login` or `use` finishes the
switch first.

If `auth.json` holds a different account than expected, codexctl refuses to
save it into the profile, so one account can never overwrite another.

## File-backed credentials

Switching only works when Codex stores credentials in `auth.json`, not the
system keyring. `login` and `use` add this to `$CODEX_HOME/config.toml`:

```toml
cli_auth_credentials_store = "file"
```

A keyring policy set by an administrator overrides this setting, and
codexctl can't work around it.

## Security

- Profile directories are `0700` and files are `0600` (POSIX).
- Credentials are never printed.
- Symlinked credential and state files are refused.
- `~/.codexctl` holds live credentials. Don't commit, sync or share it.

## Limitations

- **Running apps keep the old account.** Restart Codex apps after
  switching. The shared daemon needs a restart too; see
  [the daemon](daemon.md).
- **One account per `CODEX_HOME`.** Two apps using the same `CODEX_HOME`
  always share the active account. Use separate `CODEX_HOME` directories to
  run accounts in parallel.
- **Don't switch with `codex logout`.** It ends the session on the server.
  Use `codexctl use` to switch, and `codexctl logout` only when you want
  the session gone.
