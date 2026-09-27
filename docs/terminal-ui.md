# Terminal UI

## Dashboard

Run `codexctl` with no arguments on a terminal. Each profile shows how much of
its usage limits is left; they load in the background when the dashboard opens.

| Key | Action |
| --- | --- |
| `↑` `↓` | Move |
| `Home` `End` | Jump to the first or last profile |
| `Enter` | Switch to the profile |
| `r` | Rename |
| `d` | Remove |
| `l` | Log out |
| `n` | Log in to a new profile |
| `i` | Import the current Codex login |
| `R` | Restart the Codex daemon (shown when one is running) |
| `f` | Refresh usage limits |
| `D` | Run `doctor` |
| `U` | Run `update` |
| `q` | Quit |

## Prompts

When you leave out an argument on a terminal, codexctl asks for it:

- **Pick a profile:** `use`, `show`, `rename` and `logout` show a list.
  Type to filter it.
- **Pick several:** `remove` shows checkboxes. `Space` toggles one and
  `Ctrl-A` toggles all.
- **Type a name:** `login`, `import` and `rename` check the name as you
  type.
- **Sign in:** `login` asks for the method. API keys and tokens go into a
  masked field, so they stay out of your shell history.

Anything that deletes data asks first, with the button in red. Pressing
`Esc` in a prompt cancels the command, which exits with status 130.

## Turning it off

The UI only appears on a terminal. Pipes, redirects and `--json` always get
plain text.

| Setting | Effect |
| --- | --- |
| `CODEXCTL_NO_TUI=1` | Plain text and line prompts everywhere |
| `TERM=dumb` | Same as above |
| `NO_COLOR=1` | Keep the UI, drop the colors |
