# BootHub

**Turn Boot.dev submissions into green squares on your GitHub contribution graph.**

Same idea as NeetCode’s GitHub sync — but for [Boot.dev](https://www.boot.dev/), as a local CLI you control. No Boot.dev source access required.

<p align="center">
  <code>bootdev submit</code> → <code>boothub</code> → commit → GitHub turns green
</p>

## One-step install

```sh
curl -fsSL https://raw.githubusercontent.com/Srikumar6529/boothub/main/install.sh | bash
```

That will:

1. Install the `boothub` binary
2. Check for `git`, `gh`, and Go
3. Launch `boothub setup` (auto-detects your Boot.dev handle + timezone — press Enter to accept)

### Prerequisites

| Tool | Why |
|---|---|
| [Go](https://go.dev/dl/) | builds/installs BootHub |
| [git](https://git-scm.com/) | commits markers |
| [GitHub CLI](https://cli.github.com/) (`gh auth login`) | creates your submissions repo |
| [Boot.dev CLI](https://github.com/bootdotdev/bootdev) (`bootdev login`) | submits lessons |

Install Boot.dev CLI if you don’t have it yet:

```sh
go install github.com/bootdotdev/bootdev@latest
bootdev login
```

### Manual install

```sh
go install github.com/Srikumar6529/boothub@latest
boothub setup
```

Keep it up to date with:

```sh
boothub upgrade          # rebuilds+installs the latest version
boothub upgrade --check  # just show current vs latest
```

## Daily usage

```sh
boothub submit               # or: boothub submit <UUID>
boothub run -s               # syncs only when -s/--submit is used and checks pass
boothub sync --lesson <id>   # manual marker (web lessons)
boothub backfill             # incremental, cached intensity match
boothub repair --yes         # rebuild history for an exact match (one-time fix)
boothub upgrade              # update to the latest version
boothub config               # show status
```

Failed Boot.dev submissions do **not** create commits. If a GitHub push fails after a pass, BootHub warns but still returns Boot.dev’s exit code.

Any flag you pass to `boothub submit`/`boothub run` is forwarded to `bootdev` untouched, so `boothub run -s --course 3` works exactly like `bootdev run -s --course 3`.

## What it does

```
you
 └─ boothub submit / run -s
     └─ bootdev (child process, full TUI)
         └─ on success → write submissions/YYYY-MM-DD/<id>.json
             └─ git commit + push → green square
```

### Exact-match backfill

BootHub makes the number of commits on each day **equal to the activity count Boot.dev shows** for that day.

```http
GET https://api.boot.dev/v1/users/public/{handle}/activity_heatmap?timezone=...
```

- Days you already have commits for are topped up by exactly the difference.
- Commits already made by `boothub submit` count toward the day, so nothing is double-counted.
- A `--max-per-day` cap is available if you don’t want a literal match:

```sh
boothub backfill --max-per-day 50
```

### Fast by default (cache + incremental)

The full heatmap is fetched **once a day and cached**. After that:

- Past days never change, so they are served from the local cache.
- Only missing commits are created (`target − existing` per day).
- Repeated runs with nothing to do take well under a second and make no API calls.

Useful flags:

```sh
boothub backfill --dry-run      # preview without committing
boothub backfill --refresh      # force an API refresh
boothub backfill --no-refresh   # never call the API (offline)
boothub backfill --since 2026-01-01 --until 2026-06-30
```

### Repairing an old, drifted history

Earlier versions of BootHub could shift commits by a day or create duplicates, so the graph no longer matched Boot.dev. `repair` rebuilds the submissions repository so every day has exactly the right number of commits:

```sh
boothub repair --dry-run   # show the plan first
boothub repair --yes       # rebuild + force-push
```

- Old history is preserved on a `boothub-backup-*` branch (locally **and** on GitHub).
- Real submission markers are kept; only surplus/noise commits are dropped.
- Builds thousands of commits in one `git fast-import` pass, so it takes seconds.

### Small and fast

BootHub is a single static binary. The only dependency is a YAML parser for the config file. Anything that touches many commits (backfill, repair) is piped through one `git fast-import` subprocess instead of one `git commit` per commit, so syncing thousands of days takes seconds rather than minutes, and repeat runs make zero API calls.


## Setup UX

`boothub setup` auto-detects:

- **Handle** — from your `bootdev login` session (`GET /v1/users` with the CLI token). The official CLI does not store the handle itself.
- **Timezone** — from your OS (`/etc/localtime` / `TZ`), e.g. `America/Chicago`

Press Enter to accept defaults. Override only if needed. When piped through `curl | bash`, prompts read from `/dev/tty` so they still work.

## Config

`~/.config/boothub/config.yaml`

```yaml
repo_path: /Users/you/bootdev-submissions
remote: origin
branch: main
bootdev_handle: yourhandle
timezone: America/Chicago
enabled: true
```

The heatmap cache lives in your OS cache directory (e.g. `~/Library/Caches/boothub/`).

## Marker format

```
submissions/YYYY-MM-DD/<lesson-id>.json
```

Reserved for a future browser extension (`"trigger": "extension"`).

## FAQ

**My graph doesn’t match yet.**  
GitHub’s contribution calendar updates with a short delay (usually minutes, sometimes hours). The current day often lags. Also, GitHub counts contributions from **all** your repos, so days where you also push elsewhere will be higher than Boot.dev — that is expected. BootHub makes the *submissions repo* match exactly.

**Commits aren’t showing up at all.**  
If you chose a **private** repo, enable GitHub → Settings → Profile → **“Include private contributions on my profile”**. Commits also must be authored by an email linked to your account — BootHub uses your `gh auth` identity, so run `gh auth login` with the right account.

**Commits show up as the wrong GitHub user?**  
BootHub authors and pushes with your **`gh auth` account**, not your global git `user.email` / SSH key. Identity is set only inside the `bootdev-submissions` repo. If it looks wrong, fix `gh auth login`, then run `boothub repair --yes`.

**Doesn’t Boot.dev already sync GitHub?**  
Their “GitHub Auth” feeds *Boot.dev’s* streak from *your* commits. BootHub does the reverse: Boot.dev activity → *your* contribution graph.

**Will this commit my solution code?**  
No. Only tiny JSON markers — enough to turn the graph green.

**Private by default?**  
Yes. Setup creates a private `bootdev-submissions` repo unless you choose otherwise.

**Can I use this for web lessons?**  
Until a browser extension ships, use `boothub sync --lesson <id>` after you complete a web lesson, or just run `boothub backfill` which fills in the difference.

## Unofficial

Not affiliated with Boot.dev. Built by a learner who wanted green squares.

## License

MIT
