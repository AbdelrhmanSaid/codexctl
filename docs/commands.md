# Commands

| Command | What it does |
| --- | --- |
| `login NAME` | Log in with Codex and save the account as a profile |
| `import NAME` | Save the login Codex already has as a profile |
| `use NAME` | Switch to a profile |
| `list` (`ls`) | List profiles; `-v` adds account details |
| `current` | Print the active profile |
| `show NAME` | Show a profile's account details |
| `usage [NAME...]` | Show how much of each account's usage limits is left |
| `sync` | Save refreshed tokens into the active profile |
| `rename OLD NEW` | Rename a profile |
| `remove NAME...` (`rm`) | Delete profiles |
| `logout NAME` | End the account's session and delete the profile |
| `doctor` | Check configuration and profiles |
| `restart-daemon` | Make the Codex daemon load the active account ([why](daemon.md)) |
| `update` | Update codexctl ([details](installation.md#updating)) |
| `uninstall` | Remove codexctl ([details](installation.md#uninstalling)) |
| `completion SHELL` | Print shell completion |

On a terminal, you can leave out profile names and pick from a list
instead. See [Terminal UI](terminal-ui.md).

## Scripting

- `list`, `current`, `show`, `usage` and `doctor` accept `--json`.
- Piped output is plain text, and never prompts.
- `doctor` exits with status 1 when it finds a problem.
- `current --json` reports `"daemon_stale": true` when the Codex daemon
  still uses the previous account.

## Details

**login** runs the official `codex login` in a temporary directory, so a
failed or cancelled login changes nothing.

| Flag | Login method |
| --- | --- |
| (none) | Browser |
| `--device-auth` | Device code |
| `--with-api-key` | API key read from stdin |
| `--with-access-token` | Access token read from stdin |

Logging in to an existing profile name logs that profile in again.

**import** refuses an account that's already saved under another name.

**use** saves the current profile's refreshed tokens before it switches.
Restart running Codex apps afterwards so they pick up the new account.

**remove** leaves `auth.json` in place, so Codex stays logged in until you
switch to another profile.

**logout** runs the official `codex logout` for that account only. If it
was the active profile, `auth.json` is removed too.

**show** and `list -v` print the account ID, email, plan and last refresh
time. They never print tokens or keys.

**usage** asks Codex (`codex app-server`) for each account's 5-hour and weekly
limits. The selected profile is checked through the real Codex home. Every
other profile is checked in an isolated copy, and credentials Codex refreshes
there are saved back into the profile, since a refresh retires the old token.
API key profiles have no limits and are skipped.
