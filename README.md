# tuicheck

A modern, read-only terminal UI for [CheckMK](https://checkmk.com). It shows
what your dashboard shows — problems first — with fzf-style fuzzy search,
talking to the CheckMK **REST API** over HTTP(S). No SSH or site access needed;
any user that can see the web interface can use it.

```
 tuicheck  mysite                                          updated 14:16:54
 Hosts: 2 UP 1 DOWN   Services: 902 OK 1 WARN 2 CRIT 1 UNKNOWN
 [1 Problems]  2 Services   3 Hosts
 STATE F HOST     SERVICE                    AGE  OUTPUT
 DOWN    db02                                5m   CRIT - Ping timed out after 3s
 CRIT    web01    HTTPS certificate          2h   CRIT - certificate expires in 3 days
 UNKN    db02    PostgreSQL DB connections   5m   UNKNOWN - connection refused
```

## Install

```sh
go build -o tuicheck .
# or
go install tuicheck
```

## Configure

Create `~/.config/tuicheck/config.toml` (see `config.example.toml`):

```toml
url = "https://monitoring.example.com/mysite"
username = "automation"
password_cmd = "pass show checkmk/automation"   # stdout = password
```

`password_cmd` is any shell command that prints the password (pass, gopass,
`security find-generic-password`, 1Password CLI, …), so no secret lives in the
config file. A plain `password = "..."` is also accepted.

Other options: `refresh_seconds` (default 120, 0 = off), `insecure_tls`.
Config path override: `-config <path>` flag or `TUICHECK_CONFIG` env var.

The user needs no special role — read access to monitoring is enough. An
[automation user](https://docs.checkmk.com/latest/en/wato_user.html#automation)
is recommended.

## Keys

| Key | Action |
|---|---|
| `1` `2` `3` / `tab` | Switch view: Problems · Services · Hosts |
| `/` | Fuzzy search (matches state, host, service and plugin output — e.g. `crit nfs`) |
| `enter` | Detail view of selected host/service |
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
`ctrl` combinations for themselves — no binding above needs a modifier.
| `h` | Problems view: toggle showing handled (acked / in downtime) |
| `r` | Refresh now |
| `q` | Quit |

The **Problems** view is the default: unhandled problems sorted by severity
(host DOWN → CRIT → UNKNOWN → WARN), newest first within each severity.
Acknowledged problems are flagged `A`, downtimes `D`.

## Gentle on the server, snappy in the terminal

tuicheck is built for large, slow sites:

- Startup and auto-refresh only fetch **hosts + non-OK services**, with the
  state filter applied **server-side** — a small, fast query.
- Site-wide service counts come from the hosts' `num_services_*` aggregates,
  so the dashboard numbers never need the full service list.
- The **full service list** (without the heavy output column) is fetched
  lazily, once, in the background on first visit to the Services view, then
  cached for the session. Fuzzy search runs locally on the cache — instant,
  zero server load. Press `r` in that view to refetch deliberately.
- Opening a cached service's detail fetches **just that one service's** live
  output.

## Notes

- Works against CheckMK 2.x (REST API 1.0), all editions.
- Read-only: tuicheck only issues GET requests.
