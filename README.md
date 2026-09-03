# tuichk

A read-only terminal UI for [CheckMK](https://checkmk.com), showing the same
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

Other options: `refresh_seconds` (default 120, 0 = off), `insecure_tls`.
Config path override: `-config <path>` flag or `TUICHK_CONFIG` env var.

The user needs no special role; read access to monitoring is enough. An
[automation user](https://docs.checkmk.com/latest/en/wato_user.html#automation)
is recommended.

## Keys

| Key | Action |
|---|---|
| `1` `2` `3` `4` / `tab` | Switch view: Problems · Down · Services · Hosts |
| `/` | Fuzzy search over state, host, service and plugin output — e.g. `crit nfs`; a `!term` excludes matches, fzf-style, e.g. `nfs !ssd` |
| `enter` | Detail view of selected host/service |
| `tab` (in detail) | Jump from a service to its host and back; a host's detail lists all its services |
| `esc` | Clear search / close detail |
| `j`/`k` / arrows | Move one row |
| `d`/`u` (or `ctrl+d`/`ctrl+u`) | Half page down / up |
| `f`/`b` (or `ctrl+f`/`ctrl+b`, PgDn/PgUp) | Full page down / up |
| `g`/`G`, Home/End | Jump to top / bottom |
| `H`/`M`/`L` | Cursor to top / middle / bottom of screen |
| `zz`/`zt`/`zb` | Scroll cursor line to center / top / bottom |

While typing a search, the arrow keys (or `ctrl+j`/`ctrl+k`, `ctrl+n`/`ctrl+p`)
move the selection without leaving the input. The detail view scrolls with
`j`/`k`, `d`/`u`, `f`/`b`, `g`/`G`.

Plain-key paging exists because terminal multiplexers like zellij bind many
`ctrl` combinations for themselves, so no binding above needs a modifier.

Quitting is deliberate so a stray keypress can't kill a session you keep
running all day: only `:q` (and friends: `:quit`, `:q!`) or a double
`ctrl+c` exit. A single `ctrl+c` asks for confirmation in the footer.
| `h` | Problems view: toggle showing handled (acked / in downtime) |
| `r` | Refresh now |
| `?` or `:help` | Full key & command reference |
| `:q` | Quit (vim-style; `ctrl+c` twice also works) |

The Problems view is the default. Unhandled problems are sorted by severity
(host DOWN, then CRIT, UNKNOWN, WARN), newest first within each severity.
Acknowledged problems are flagged `A`, ones in downtime `D`.

The Down view lists every non-UP host, including acknowledged hosts and hosts
in downtime that the Problems view hides as handled.

Crit-level problems (CRIT services, DOWN hosts) between 15 minutes and 4 hours
old get an inverted red badge and sort above the others, on the assumption
that they are recent enough to still be worth acting on. The bounds are
configurable with `hot_min` and `hot_max`.

The `:` command line accepts `:q` (and `:quit`, `:q!`), the view names
`:problems` `:down` `:services` `:hosts` (or `:1`–`:4`, `:p` `:d` `:s`),
`:r`/`:refresh` (`:r!` also refetches the full service list), `:handled`,
`:N` to jump to row N, and `:help`.

State filters `:crit`, `:warn`, `:unknown` (and `:ok`, `:all` to clear)
restrict the view to one state before fuzzy search runs, so `/nfs` under
`:crit` cannot turn up a WARN row the way `crit nfs` alone might. DOWN hosts
count as CRIT, UNREACHABLE as UNKNOWN. The active filter is shown next to the
tabs.

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
  state filter runs server-side, so the response stays small.
- The host/service counts in the summary line come from each host's
  `num_services_*` columns, not from a service query.
- The full service list is fetched once, in the background, the first time you
  open the Services view, and cached for the session. Fuzzy search then runs
  locally. Press `r` there to refetch.
- Opening a service's detail fetches that one service's output; opening a
  host's detail fetches that one host's services.

## Notes

- Works against CheckMK 2.x (REST API 1.0), all editions.
- Read-only: tuichk only issues GET requests.
