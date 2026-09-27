# codexctl

Switch between Codex accounts with one command.

codexctl saves each Codex login as a named profile and swaps the active
`auth.json` when you switch. Everything else in `CODEX_HOME` (config,
history, sessions, skills, plugins) stays shared. It runs entirely on your
machine.

## Install

```console
# Linux, macOS, FreeBSD, WSL
curl -fsSL https://raw.githubusercontent.com/AbdelrhmanSaid/codexctl/master/install.sh | sh
```

```powershell
# Windows
irm https://raw.githubusercontent.com/AbdelrhmanSaid/codexctl/master/install.ps1 | iex
```

Other options (Go, Linux packages, prebuilt binaries) are in
[docs/installation.md](docs/installation.md).

## Quick start

```console
codexctl login work          # log in and save the account as "work"
codexctl import personal     # or save the login Codex already has
codexctl use personal        # switch accounts
codexctl list                # see saved profiles
```

On a terminal, run `codexctl` on its own for an interactive dashboard.

## Documentation

- [Installation, updates and uninstalling](docs/installation.md)
- [Commands](docs/commands.md)
- [Terminal UI](docs/terminal-ui.md)
- [The Codex app-server daemon](docs/daemon.md): read this if accounts don't switch in your IDE or app
- [How it works](docs/how-it-works.md)
- [Releasing](docs/releasing.md) (for maintainers)

## License

[MIT](LICENSE)
