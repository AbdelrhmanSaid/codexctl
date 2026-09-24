# codexctl

`codexctl` is a small, local-only profile switcher for Codex accounts.

```console
codexctl login personal
codexctl login work --device-auth
codexctl import existing        # save the login Codex already has
codexctl use personal
codexctl list -v
codexctl current
codexctl show work
codexctl sync
codexctl rename personal home
codexctl remove work
codexctl logout existing
```

`list`, `current`, `show`, and `doctor` accept `--json` for scripting.

It keeps Codex's normal configuration, history, sessions, skills, and plugins in
the same `CODEX_HOME`. Only the file-backed login cache is switched. This makes
the selected account visible to the Codex CLI and other Codex clients that use
the same `CODEX_HOME`.

## Install

### With Go

```console
go install github.com/AbdelrhmanSaid/codexctl@latest
```

This requires Go 1.24 or newer. Make sure Go's binary directory (usually
`$HOME/go/bin`) is on `PATH`.

### Prebuilt binaries

Download the archive for your operating system and CPU from the
[latest GitHub release](https://github.com/AbdelrhmanSaid/codexctl/releases/latest),
extract it, and move `codexctl` (or `codexctl.exe` on Windows) to a directory on
`PATH`. Releases are provided for macOS, Linux, Windows, and FreeBSD on x86-64
and ARM64. SHA-256 hashes are published in `checksums.txt` with every release.

Linux releases also include native packages:

- Debian and Ubuntu: download the matching `.deb`, then run
  `sudo apt install ./codexctl_*.deb`.
- Fedora, RHEL, and related distributions: download the matching `.rpm`, then
  run `sudo rpm -i codexctl_*.rpm`.
- Alpine Linux: download the matching `.apk`, then run
  `sudo apk add --allow-untrusted ./codexctl_*.apk`.
- Arch Linux: download the matching `.pkg.tar.zst`, then run
  `sudo pacman -U ./codexctl_*.pkg.tar.zst`.

### Build locally

```console
go build -o codexctl .
```

Shell completion is available through `codexctl completion bash`, `zsh`,
`fish`, or `powershell`.

## Releasing

Pushing a semantic-version tag creates a GitHub release with native archives,
Linux packages, and checksums:

```console
git tag -a v0.1.0 -m "codexctl v0.1.0"
git push origin v0.1.0
```

To validate the release locally without publishing it, install GoReleaser and
run:

```console
goreleaser check
goreleaser release --snapshot --clean
```

## How it works

Profiles are stored as private files:

```text
~/.codexctl/
├── current
├── lock
└── profiles/
    ├── personal.json
    └── work.json
```

`lock` serializes codexctl operations. It is an OS-level file lock, so it is
released automatically if codexctl crashes or is interrupted; the file itself stays in place and never needs to be removed.

`codexctl login NAME` runs the official `codex login` command with an isolated
temporary `CODEX_HOME`. A successful login is validated, saved, and activated.
A cancelled or failed login leaves the active account untouched.

`codexctl use NAME` atomically copies that profile to
`$CODEX_HOME/auth.json` (normally `~/.codex/auth.json`). Before switching away,
it saves any token refreshes Codex wrote for the current profile. If the account
ID unexpectedly changed, it refuses to overwrite the saved profile.

A switch also records a short-lived recovery marker before updating
`auth.json` and `current`. If the process is interrupted between those writes,
the next successful `login` or `use` completes the interrupted switch before
continuing.

`codexctl import NAME` saves the `auth.json` Codex is already using as a
profile and selects it, for accounts that were logged in with plain
`codex login`. It refuses an account that is already saved under another name.

`codexctl sync` saves token refreshes from the active `auth.json` into the
selected profile on demand. `login` and `use` do this automatically before
switching away.

`codexctl show NAME` and `codexctl list -v` print the account ID, email, plan,
and last refresh time read from the saved snapshot. Tokens and API keys are
never printed.

`codexctl rename OLD NEW` renames a saved profile. If it is the selected
profile, any token refreshes are saved first and the selection follows the new
name. `codexctl remove NAME` deletes a saved profile. Removing the selected
profile clears the selection but leaves `auth.json` in place, so Codex stays
logged in until you `use` another profile.

`codexctl logout NAME` ends the account's session: it runs the official
`codex logout` in an isolated temporary `CODEX_HOME` that holds only that
profile's credentials, then deletes the profile. If the profile was selected
and the active `auth.json` holds the same account, that file is removed too,
since its tokens are no longer usable.

Because swapping `auth.json` only works with file-backed credentials, login and
use ensure this root setting exists in `$CODEX_HOME/config.toml`:

```toml
cli_auth_credentials_store = "file"
```

Profile directories use mode `0700` and credential/state files use `0600` on
POSIX systems. Credential contents are never printed. Symlinked credential and
state paths are refused.

## Important behavior

- Restart already-running Codex CLI, IDE, or app processes after switching;
  they may retain credentials in memory.
- Do not use `codex logout` to switch profiles. Logout is broader than a local
  file swap and can invalidate a session you intended to keep. Use
  `codexctl logout NAME` when you do want to end a specific account's session.
- Two simultaneously running clients that use the same `CODEX_HOME` still share
  one active account. Separate `CODEX_HOME` directories are required for truly
  parallel accounts.
- An administrator-enforced keyring credential policy can override user config;
  `codexctl doctor` catches the common local configuration problems, but cannot
  override managed policy.
- `auth.json` contains live credentials. Do not commit, sync, or share
  `~/.codexctl`.

## License

[MIT](LICENSE)
