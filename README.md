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
codexctl restart-daemon         # make a running Codex daemon reload the switch
```

`list`, `current`, `show`, and `doctor` accept `--json` for scripting.

It keeps Codex's normal configuration, history, sessions, skills, and plugins in
the same `CODEX_HOME`. Only the file-backed login cache is switched. This makes
the selected account visible to the Codex CLI and other Codex clients that use
the same `CODEX_HOME`.

## Install

### Install script

On Linux, macOS, FreeBSD, and WSL:

```console
curl -fsSL https://raw.githubusercontent.com/AbdelrhmanSaid/codexctl/master/install.sh | sh
```

On Windows, from PowerShell:

```powershell
irm https://raw.githubusercontent.com/AbdelrhmanSaid/codexctl/master/install.ps1 | iex
```

Both scripts download the release archive for your platform, check its
SHA-256 against the release's `checksums.txt`, and install the `codexctl`
executable. `install.sh` also verifies the Ed25519 signature on
`checksums.txt` when the local `openssl` supports it. On Unix systems the
executable goes to `/usr/local/bin` when that is writable, otherwise to
`~/.local/bin`. On Windows it goes to `%LOCALAPPDATA%\Programs\codexctl`,
which is added to your user `PATH`.

Options pick a release or an install directory:

```console
curl -fsSL https://raw.githubusercontent.com/AbdelrhmanSaid/codexctl/master/install.sh | sh -s -- --version 0.2.1 --dir ~/bin
```

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/AbdelrhmanSaid/codexctl/master/install.ps1))) -Version 0.2.1 -InstallDir C:\Tools\codexctl
```

The same settings can be given as the `CODEXCTL_VERSION` and
`CODEXCTL_INSTALL_DIR` environment variables. Run the script with `--help`
(or read the header of `install.ps1`) for the full list.

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

### Updating

```console
codexctl update --check   # report whether a newer release exists
codexctl update           # download, verify, and replace this executable
codexctl update --to 0.2.1
```

`update` downloads the release archive for the current platform from GitHub,
checks its SHA-256 against the release's `checksums.txt`, and verifies the
Ed25519 signature on that file with a public key built into the binary. A
release that fails either check is never installed. The new executable is
written next to the old one and renamed over it; on Windows the old file is
moved aside as `codexctl.exe.old` and cleaned up by the next update.

Binaries installed with a Linux package or `go install` are told to update the
same way they were installed. `--force` overrides that, and is also required to
downgrade with `--to`. `update` never contacts the network unless you run it.

## Releasing

Pushing a semantic-version tag creates a GitHub release with native archives,
Linux packages, and checksums:

```console
git tag -a v0.2.1 -m "codexctl v0.2.1"
git push origin v0.2.1
```

Releases are signed so that `codexctl update` can verify them. The Ed25519
private key lives in the `CODEXCTL_SIGNING_KEY` repository secret and the
matching public key is embedded in `internal/update/sign.go`. To create a key:

```console
go run ./tools/sign keygen -out signing.key
gh secret set CODEXCTL_SIGNING_KEY < signing.key
```

Then paste the printed public key into `internal/update/sign.go`. Binaries
only trust the key they were built with, so rotate a key by publishing one
release, signed with the old key, that embeds the new one.

To validate the release locally without publishing it, install GoReleaser and
run:

```console
goreleaser check
CODEXCTL_SIGNING_KEY=$(cat signing.key) goreleaser release --snapshot --clean
go run ./tools/sign verify dist/checksums.txt
```

Snapshot builds in CI skip signing.

## How it works

Profiles are stored as private files:

```text
~/.codexctl/
├── current
├── lock
├── switched
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

## The Codex app-server daemon

Codex v0.157.0 and later runs a shared app-server daemon in the background
and connects the CLI, IDE extensions, and the desktop app to it. The daemon
reads `auth.json` once, when it starts, and keeps those credentials in memory.
It does not watch the file, and its protocol has no request that reloads it,
so after `codexctl use` a running daemon keeps using the previous account
until it is restarted. Plain `codex login` has the same gap.

codexctl handles this as follows:

- After every command that changes `auth.json` (`login`, `use`, `logout` of
  the selected profile, or any command that finishes an interrupted switch),
  codexctl checks whether a daemon is running for the same `CODEX_HOME`. If
  one is, it says so and, when run from a terminal, asks whether to restart
  it now. The default answer is no. In scripts and pipes it never asks and
  never restarts; it prints a reminder instead.
- `codexctl restart-daemon` restarts the daemon on demand. It runs
  `codex app-server daemon restart` with the same `CODEX_HOME`.
- A restart interrupts every Codex session that runs on the daemon. Codex
  tries to resume interrupted threads after a restart, but a turn that was in
  progress may be lost, so let running work finish first.
- codexctl never restarts a daemon silently and never starts one. Since
  `codex app-server daemon restart` would start a daemon when none is
  running, codexctl restarts only when the daemon's pid file names a live
  process *and* its control socket accepts a connection. If those disagree,
  it reports why and does nothing.

### Detecting and resolving a mismatch

`codexctl current` and `codexctl doctor` warn when the running daemon started
before codexctl last changed `auth.json`:

```console
$ codexctl doctor
ok   file-backed credential storage is configured
ok   active auth.json is valid JSON and is not a symlink
ok   2 saved profile(s)
ok   active auth.json matches profile work
warn Codex app-server daemon (pid 12345) started before codexctl switched to profile "work" and still uses the previous credentials; run 'codexctl restart-daemon' to reload it (this interrupts active Codex sessions)
```

`codexctl current --json` reports the same condition as `"daemon_stale": true`.

The check compares the daemon's start time with the time recorded in
`~/.codexctl/switched`, which codexctl writes whenever it changes
`auth.json`. That file holds the `CODEX_HOME`, the profile name, and a
timestamp, never credentials. The daemon's start time is read from its pid
file on macOS and taken from that file's modification time elsewhere.
codexctl does not ask the daemon which account it holds: that would need a
WebSocket client for the app-server protocol, and the daemon's account query
contacts the network.

To resolve a mismatch, run `codexctl restart-daemon` once no Codex session is
doing work you would miss. Until then the daemon keeps using the previous
account, and if it refreshes that account's tokens it writes them to
`auth.json` over the newly selected profile. codexctl's account-ID check
refuses to save such a file into the selected profile, so saved profiles stay
intact, and running `codexctl use NAME` again restores the right credentials.

## Important behavior

- Restart already-running Codex CLI, IDE, or app processes after switching;
  they may retain credentials in memory. The shared app-server daemon always
  does; see above.
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
