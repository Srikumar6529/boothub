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
go install github.com/Srikumar6529/boothub/cmd/boothub@latest
boothub setup
```

## Daily usage

```sh
boothub submit          # or: boothub submit <UUID>
boothub run -s          # syncs only when -s/--submit is used and checks pass
boothub sync --lesson <id>   # manual marker (web lessons)
boothub backfill        # intensity-match past heatmap days onto GitHub
boothub config          # show status
```

Failed Boot.dev submissions do **not** create commits. If a GitHub push fails after a pass, BootHub warns but still returns Boot.dev’s exit code.

## What it does

```
you
 └─ boothub submit / run -s
     └─ bootdev (child process, full TUI)
         └─ on success → write submissions/YYYY-MM-DD/<id>.json
             └─ git commit + push → green square
```

### Backfill (the fun part)

Your Boot.dev profile heatmap is public. BootHub fetches:

```http
GET https://api.boot.dev/v1/users/public/{handle}/activity_heatmap?timezone=...
```

For each day with activity count `N`, it creates **N backdated commits** so GitHub intensity matches your Boot.dev heatmap. Already-synced days are skipped (idempotent).

Cap extreme days if you want:

```sh
boothub backfill --max-per-day 50
```

## Setup UX

`boothub setup` auto-detects:

- **Handle** — from your `bootdev login` session (`GET /v1/users` with the CLI token). The official CLI does not store the handle itself.
- **Timezone** — from your OS (`/etc/localtime` / `TZ`), e.g. `America/Chicago`

Press Enter to accept defaults. Override only if needed.

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

## Marker format

```
submissions/YYYY-MM-DD/<lesson-id>.json
```

Reserved for a future browser extension (`"trigger": "extension"`).

## FAQ

**Doesn’t Boot.dev already sync GitHub?**  
Their “GitHub Auth” feeds *Boot.dev’s* streak from *your* commits. BootHub does the reverse: Boot.dev activity → *your* contribution graph.

**Will this commit my solution code?**  
No. Only tiny JSON markers — enough to turn the graph green.

**Private by default?**  
Yes. Setup creates a private `bootdev-submissions` repo unless you choose otherwise.

**Can I use this for web lessons?**  
Until a browser extension ships, use `boothub sync --lesson <id>` after you complete a web lesson.

## Unofficial

Not affiliated with Boot.dev. Built by a learner who wanted green squares.

## License

MIT
