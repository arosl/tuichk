# tuichk

A terminal UI for [CheckMK](https://checkmk.com), showing the same
status your dashboard does, problems first, with fzf-style fuzzy search. It
talks to the CheckMK REST API over HTTP(S); no SSH or site access is needed,
and any user who can log into the web interface can use it.

```
 tuichk  mysite                                          updated 14:16:54
 Hosts: 2 UP 1 DOWN   Services: 902 OK 1 WARN 2 CRIT 1 UNKNOWN
 [1 Problems]  2 Down   3 Services   4 Hosts
 STATE F HOST     SERVICE                    AGE  OUTPUT
 DOWN    db02                                5m   CRIT - Ping timed out after 3s
 CRIT    web01    HTTPS certificate          2h   CRIT - certificate expires in 3 days
 UNKN    db02    PostgreSQL DB connections   5m   UNKNOWN - connection refused
```

## Install

```sh
make install          # stripped binary into ~/.local/bin
make install PREFIX=/usr/local
# or plain go:
go build -o tuichk .
```

## Configure

Create `~/.config/tuichk/config.toml` (see `config.example.toml`):

```toml
url = "https://monitoring.example.com/mysite"
username = "automation"
password_cmd = "pass show checkmk/automation"   # stdout = password
```

`password_cmd` is any shell command that prints the password (pass, gopass,
`security find-generic-password`, 1Password CLI, …), so no secret lives in the
config file. A plain `password = "..."` is also accepted.

Other options: `refresh_seconds` (default 120, 0 = off), `insecure_tls`,
`mouse` (default off, see below).
Config path override: `-config <path>` flag or `TUICHK_CONFIG` env var.

The user needs no special role; read access to monitoring is enough. An
[automation user](https://docs.checkmk.com/latest/en/wato_user.html#automation)
is recommended.

## Keys

| Key | Action |
|---|---|
| `tab` / `shift+tab` | Next / previous view: Problems · Down · Services · Hosts |
| `/` | Fuzzy search over state, host, service and plugin output — e.g. `crit nfs`; a `!term` excludes matches, fzf-style, e.g. `nfs !ssd` |
| `enter` | Detail view of selected host/service |
| `tab` (in detail) | Jump from a service to its host and back; a host's detail lists all its services |
| `esc` | Clear search / close detail |
| `j`/`k` / arrows | Move one row |
| `12G` `5j` `3d` … | Count prefix, vim-style: jump to row 12, five rows down, three half pages; the pending count shows in the footer |
| `d`/`u` (or `ctrl+d`/`ctrl+u`) | Half page down / up |
| `f`/`b` (or `ctrl+f`/`ctrl+b`, PgDn/PgUp) | Full page down / up |
| `g`/`G`, Home/End | Jump to top / bottom |
| `H`/`M`/`L` | Cursor to top / middle / bottom of screen |
| `zz`/`zt`/`zb` | Scroll cursor line to center / top / bottom |
| `r` | Refresh now |
| `?` or `:help` | Full key & command reference; scrolls with the movement keys, `esc` closes |
| `:q` | Quit (vim-style; `ctrl+c` twice also works) |

While typing a search, the arrow keys (or `ctrl+j`/`ctrl+k`, `ctrl+n`/`ctrl+p`)
move the selection without leaving the input. The detail view scrolls with
`j`/`k`, `d`/`u`, `f`/`b`, `g`/`G`.

Plain-key paging exists because terminal multiplexers like zellij bind many
`ctrl` combinations for themselves, so no binding above needs a modifier.

Quitting is deliberate so a stray keypress can't kill a session you keep
running all day: only `:q` (and friends: `:quit`, `:q!`) or a double
`ctrl+c` exit. A single `ctrl+c` asks for confirmation in the footer.

The Problems view is the default. Unhandled problems are sorted by severity
(host DOWN, then CRIT, UNKNOWN, WARN), newest first within each severity.
Acknowledged problems are flagged `A`, ones in downtime `D`; the Problems
view hides them unless `:handled` is toggled on.

The Down view lists every non-UP host, including acknowledged hosts and hosts
in downtime that the Problems view hides as handled.

Crit-level problems (CRIT services, DOWN hosts) between 15 minutes and 4 hours
old get an inverted red badge and sort above the others, on the assumption
that they are recent enough to still be worth acting on. The bounds are
configurable with `hot_min` and `hot_max`.

The `:` command line has tab completion (repeat `tab` to cycle the matches)
and up/down recall of the session's earlier commands. It accepts `:q` (and `:quit`, `:q!`), the view names
`:problems` `:down` `:services` `:hosts` (or `:p` `:d` `:s`),
`:r`/`:refresh` (`:r!` also refetches the full service list), `:handled`,
`:mouse`, `:browser`, `:wiki`, `:ssh` (see below),
`:N` to jump to row N, like `NG` (`:numbers` or `:nu` shows the numbers),
and `:help`.

State filters `:crit`, `:warn`, `:unknown` (and `:ok`, `:all` to clear)
restrict the view to one state before fuzzy search runs, so `/nfs` under
`:crit` cannot turn up a WARN row the way `crit nfs` alone might. DOWN hosts
count as CRIT, UNREACHABLE as UNKNOWN. The active filter is shown next to the
tabs.

## Mouse

Mouse support is off by default: capturing the mouse takes over the
terminal's own text selection, and copying plugin output is common enough
that plain drag-to-select stays the default. Turn it on with `mouse = true`
in the config or `:mouse` at runtime (which also turns it off again). While
on, the wheel scrolls the list and the detail view, a click selects a row, a
second click on the selected row opens it, and clicking a tab switches view.
Select text with shift-drag (alt-drag in some terminals) while it is on.

## Browser, wiki and SSH

`:browser` opens the selected host or service in the CheckMK web GUI, using
`open` on macOS and `xdg-open` elsewhere. Set `browser_cmd` to change that;
`{url}` in it is replaced by the link, already shell-quoted.

`:wiki` opens the selected host in your own wiki through the same browser
command. Set `wiki_url` to a page template: `{host}` is the full host name,
`{short}` its first DNS label, both URL-escaped. For example
`wiki_url = "https://wiki.example.com/index.php?search={short}"`.

`:ssh` opens a shell on the selected host (from the list or an open detail).
Inside zellij, tmux, WezTerm, kitty or Ghostty it opens in a new pane next to
tuichk; kitty needs `allow_remote_control yes`, Ghostty needs 1.3 or newer on
macOS, where the split is a normal shell with the ssh command typed into it
so the pane closes when ssh ends, and opens a new window on Linux.
Anywhere else tuichk steps aside, runs ssh in the same window, and
comes back when it exits. Set `ssh_inline = true` to get that everywhere.
`ssh_cmd` replaces the default `ssh {host}`, e.g. `ssh -J bastion {host}`;
`{host}` is the CheckMK host name, shell-quoted. Both commands run through
`sh -c`, and the substituted value is single-quoted so a hostile host name
cannot inject shell.

## Graphs

The detail view (`enter`) draws the same graphs the web GUI shows, as braille
charts. They are plain text, so they render in any terminal, under tmux or
zellij, and over SSH.

The graph data comes from the GUI's popup endpoint. tuichk authenticates to it
with a normal browser session login using the configured user, so no
automation user or extra credentials are needed. Tested on CheckMK 2.0,
including distributed setups.

## Load on the server

tuichk is built for large sites where the API is slow. It avoids fetching the
full service list except when asked:

- Startup and auto-refresh fetch only the hosts and the non-OK services. The
  "state is not OK" condition is evaluated by the server (a Livestatus query
  filter), so it returns a few hundred rows instead of every service.
- The host/service counts in the summary line come from each host's
  `num_services_*` columns, not from a service query.
- The full service list is fetched once, in the background, the first time you
  open the Services view, and cached for the session. Fuzzy search then runs
  locally. Press `r` there to refetch.
- Opening a service's detail fetches that one service's output; opening a
  host's detail fetches that one host's services.

## Notes

- Works against CheckMK 2.x (REST API 1.0), all editions.
- Read-only against CheckMK: tuichk only issues GET requests. The only
  things it runs locally are the configured browser and ssh commands.

## License

MIT. See [LICENSE](LICENSE).
