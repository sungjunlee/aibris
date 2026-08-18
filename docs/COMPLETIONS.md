# Shell completions and man pages

aibris packages shell completions (bash, zsh, fish, PowerShell) and man pages
in every release archive. `brew install sungjunlee/tap/aibris` installs bash,
zsh, and fish completions plus man pages into the Homebrew prefix. `install.sh`
installs completions into the installing user's `~/.local` (and fish
`~/.config`) files.

## What Homebrew installs

The formula generates completions from the installed binary
(`aibris completion <shell>`) and copies man pages from the release archive:

| Kind | Homebrew prefix path |
| ---- | -------------------- |
| bash | `$(brew --prefix)/share/bash-completion/completions/aibris` |
| zsh | `$(brew --prefix)/share/zsh/site-functions/_aibris` |
| fish | `$(brew --prefix)/share/fish/vendor_completions.d/aibris.fish` |
| man | `$(brew --prefix)/share/man/man1/aibris*.1` |

Stock macOS zsh already includes Homebrew `site-functions` on `fpath`, so no
`.zshrc` change is needed for the brew path.

## What install.sh installs

After installing the binary, `install.sh` runs `aibris completion <shell>`
and writes the scripts to standard per-user locations:

| Shell | File |
| ----- | ---- |
| bash | `~/.local/share/bash-completion/completions/aibris` |
| zsh | `~/.local/share/zsh/site-functions/_aibris` |
| fish | `~/.config/fish/completions/aibris.fish` |

Installation is best-effort: a failure prints a warning and never aborts the
install. **The installer never edits shell profile files** (`.bashrc`,
`.zshrc`, `config.fish`); if a completion file is not picked up automatically,
wire up the directory yourself.

### zsh `fpath`

Stock macOS zsh and Oh My Zsh include Homebrew and system `site-functions` on
`fpath`, but they do **not** include `~/.local/share/zsh/site-functions`.
`install.sh` still writes `_aibris` there (it never edits `.zshrc`). Add the
directory yourself, **before** `compinit`:

```zsh
# ~/.zshrc — before autoload -U compinit && compinit
fpath=("$HOME/.local/share/zsh/site-functions" $fpath)
```

Then open a new shell or run `compinit`. `install.sh` prints a one-line hint
when it cannot see that path on the current `fpath`.

PowerShell completions are not auto-installed. Load them manually from the
release archive:

```powershell
aibris.exe completion powershell | Out-String | Invoke-Expression
```

## Release archives

Each release archive contains:

- `release-assets/completions/aibris.bash`, `aibris.zsh`, `aibris.fish`,
  `aibris.ps1`
- `release-assets/man/aibris.1` (plus pages for subcommands)

The man page covers `scan`, `clean`, the safety gates, and exit status.
Install it with, for example:

```bash
mkdir -p ~/.local/share/man/man1
cp release-assets/man/*.1 ~/.local/share/man/man1/
man aibris
```

## Uninstall

Delete the installed completion files:

```bash
rm -f ~/.local/share/bash-completion/completions/aibris
rm -f ~/.local/share/zsh/site-functions/_aibris
rm -f ~/.config/fish/completions/aibris.fish
```

To uninstall the binary and man pages, remove the files you copied from the
release archive. `install.sh` never wrote anything else, so nothing else
needs cleanup.

## Regenerating from source

Generated artifacts are reproducible from source (fixed file names and a
pinned man page date):

```bash
make release-assets
```

This writes the completions and man pages to `release-assets/` at the
repository root. The release pipeline (`make dist` / goreleaser) runs the
same step before packaging.
