# codexctl

`codexctl` is a small, local-only profile switcher for Codex accounts.

```console
codexctl login personal
codexctl login work --device-auth
codexctl use personal
codexctl list
codexctl current
```

It keeps Codex's normal configuration, history, sessions, skills, and plugins in
the same `CODEX_HOME`. Only the file-backed login cache is switched. This makes
the selected account visible to the Codex CLI and other Codex clients that use
the same `CODEX_HOME`.

## Install

```console
go install github.com/AbdelrhmanSaid/codexctl@latest
```

Or build a local binary:

```console
go build -o codexctl .
```

Shell completion is available through `codexctl completion bash`, `zsh`,
`fish`, or `powershell`.

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
  file swap and can invalidate a session you intended to keep.
- Two simultaneously running clients that use the same `CODEX_HOME` still share
  one active account. Separate `CODEX_HOME` directories are required for truly
  parallel accounts.
- An administrator-enforced keyring credential policy can override user config;
  `codexctl doctor` catches the common local configuration problems, but cannot
  override managed policy.
- `auth.json` contains live credentials. Do not commit, sync, or share
  `~/.codexctl`.

## Prior art and design choices

The implementation was informed by several open-source tools:

- [codex-auth-snap](https://github.com/enerai/codex-auth-snap) emphasizes strict
  permissions, locks, JSON validation, symlink refusal, and preserving refreshed
  credentials before a switch.
- [codex-auth-switch](https://github.com/kndoshn/codex-auth-switch) distinguishes
  the tool's selected profile from the account currently observed in
  `auth.json`, which may be changed externally.
- [codex-account-switcher](https://github.com/Cloud370/codex-account-switcher)
  isolates runtime homes for parallel sessions. That is useful but intentionally
  outside this first, simple auth-switching scope.
- [cx](https://github.com/ralphkrauss/codex-account-switcher) handles profile
  locks and pins file-backed credential storage because OS keyrings are not
  naturally scoped by a copied `CODEX_HOME`.

OpenAI's official documentation confirms that Codex can store cached login data
in `$CODEX_HOME/auth.json`, that CLI and IDE reuse cached login details, and that
`cli_auth_credentials_store = "file"` selects this behavior:
[Codex authentication](https://developers.openai.com/codex/auth).

## License

[MIT](LICENSE)
