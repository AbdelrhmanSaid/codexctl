# The Codex app-server daemon

Codex v0.157.0 and later runs a background daemon that the CLI, IDE
extensions and desktop app connect to. It reads `auth.json` once, at
startup, and has no way to reload it.

**So after `codexctl use`, a running daemon keeps the old account until it
restarts.** Plain `codex login` has the same problem.

## What codexctl does

- After a switch, it checks for a running daemon. On a terminal it offers to
  restart it (default: no). In scripts it only prints a reminder.
- `codexctl restart-daemon` restarts it on demand. On a terminal it asks
  first; `--yes` skips that.
- It never starts a daemon, and never restarts one without asking.

A restart interrupts every Codex session on the daemon. Codex tries to
resume them, but a turn in progress may be lost, so let running work finish
first.

## Spotting a mismatch

`codexctl current` and `codexctl doctor` warn when the daemon started before
the last switch:

```console
$ codexctl doctor
ok   file-backed credential storage is configured
ok   active auth.json is valid JSON and is not a symlink
ok   2 saved profile(s)
ok   active auth.json matches profile work
warn Codex app-server daemon (pid 12345) started before codexctl switched to profile "work" and still uses the previous credentials; run 'codexctl restart-daemon' to reload it (this interrupts active Codex sessions)
```

To fix it, run `codexctl restart-daemon` when nothing important is running.

Until you restart, your saved profiles are safe. If the old daemon writes
refreshed tokens for the wrong account, codexctl won't save them into the
active profile. Run `codexctl use NAME` again to restore the right file.

## How the check works

codexctl compares the daemon's start time with the time it last switched
accounts, which it records in `~/.codexctl/switched` (no credentials). It
doesn't ask the daemon which account it holds, because that query goes over
the network.
