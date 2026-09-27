# Installation

## Install script

```console
# Linux, macOS, FreeBSD, WSL
curl -fsSL https://raw.githubusercontent.com/AbdelrhmanSaid/codexctl/master/install.sh | sh
```

```powershell
# Windows (PowerShell)
irm https://raw.githubusercontent.com/AbdelrhmanSaid/codexctl/master/install.ps1 | iex
```

The scripts download the release for your platform and check its SHA-256.
`install.sh` also checks the release signature when `openssl` supports
Ed25519.

| Platform | Installs to |
| --- | --- |
| Linux, macOS, FreeBSD | `/usr/local/bin` if writable, else `~/.local/bin` |
| Windows | `%LOCALAPPDATA%\Programs\codexctl` (added to your user `PATH`) |

To pick a version or directory:

```console
curl -fsSL https://raw.githubusercontent.com/AbdelrhmanSaid/codexctl/master/install.sh | sh -s -- --version 0.3.0 --dir ~/bin
```

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/AbdelrhmanSaid/codexctl/master/install.ps1))) -Version 0.3.0 -InstallDir C:\Tools\codexctl
```

Or set `CODEXCTL_VERSION` and `CODEXCTL_INSTALL_DIR`. Run `install.sh
--help` for all options.

## Go

```console
go install github.com/AbdelrhmanSaid/codexctl@latest
```

Needs Go 1.24 or newer, with `$HOME/go/bin` on your `PATH`.

## Prebuilt binaries and packages

Download from the [latest release](https://github.com/AbdelrhmanSaid/codexctl/releases/latest).
Builds exist for macOS, Linux, Windows and FreeBSD on x86-64 and ARM64.
Extract the archive and put `codexctl` on your `PATH`.

Linux packages:

| Distribution | Command |
| --- | --- |
| Debian, Ubuntu | `sudo apt install ./codexctl_*.deb` |
| Fedora, RHEL | `sudo rpm -i codexctl_*.rpm` |
| Alpine | `sudo apk add --allow-untrusted ./codexctl_*.apk` |
| Arch | `sudo pacman -U ./codexctl_*.pkg.tar.zst` |

## From source

```console
go build -o codexctl .
```

## Shell completion

```console
codexctl completion bash   # or zsh, fish, powershell
```

## Updating

```console
codexctl update --check    # is there a newer release?
codexctl update            # install it
codexctl update --to 0.3.0 # install a specific version
```

`update` checks the release's signature and checksum before replacing the
binary, and refuses a release that fails either check. It only uses the
network when you run it.

If you installed with a Linux package or `go install`, update the same way
instead. `--force` overrides this, and is also needed to downgrade.

## Uninstalling

```console
codexctl uninstall                        # remove the binary, keep profiles
codexctl uninstall --purge                # also delete ~/.codexctl
codexctl uninstall --purge --keep-binary  # delete only the profiles
```

- It asks before removing anything. `--yes` skips the question, and is
  required in scripts.
- Your Codex login is untouched: the active `auth.json` stays, so Codex
  stays logged in with the current account.
- Deleted profiles can't be recovered.
- Linux packages: remove the binary with your package manager.
- Windows: remove the install directory from your `PATH` if you no longer
  need it.
