# go-novel-dl

`go-novel-dl` is a Go rewrite scaffold inspired by the architecture and command surface of `novel-downloader`.

The project is CLI-first for now. The initial version keeps the same high-level flow:

CLI -> config -> site registry -> download -> process pipeline -> export -> local storage

The current codebase focuses on project structure, command compatibility, staged storage, and extensibility. The first real site implementation is `esjzone`, including mirror-aware URL handling, login, cookie persistence, and resume-aware chapter refresh.

## Status

- CLI-first scaffold is ready
- Cobra-based command tree is in place
- `data/settings.toml` workflow is in place
- download/search/export/config/clean commands are implemented
- staged raw storage and basic export pipeline are implemented
- `esjzone` is implemented first, including mirror-host support
- ESJ Zone login, cookie persistence, and resume-aware chapter refresh are implemented
- Web UI is intentionally deferred

Important: the current downloader implementation is production-oriented for `esjzone`, while the rest of the architecture stays ready for additional sites.

## Commands

The CLI mirrors the command naming used by `novel-downloader`:

```bash
novel-cli download [book_ids | url]
novel-cli search keyword
novel-cli export [book_id ...]
novel-cli config init
novel-cli config set-lang zh_CN
novel-cli clean state
novel-cli clean logs
novel-cli clean cache
novel-cli clean book
```

### Examples

```bash
# initialize config
go run ./cmd/novel-cli config init

# download by url
go run ./cmd/novel-cli download https://www.esjzone.cc/detail/1660702902.html

# download by site + book id
go run ./cmd/novel-cli download --site esjzone 1660702902

# partial range for the first book id
go run ./cmd/novel-cli download --site esjzone 1660702902 --start 294593 --end 305803

# search and then interactively choose one result to download
go run ./cmd/novel-cli search 三体

# export downloaded books
go run ./cmd/novel-cli export --site esjzone 1660702902 --format epub

# clean logs
go run ./cmd/novel-cli clean logs --dry-run
```

## Project Layout

```text
cmd/novel-cli           CLI entrypoint
internal/cli            Cobra commands and interactive shell
internal/app            Orchestration layer for download/search/export/clean
internal/config         Config defaults, loading, merging, embedded sample config
internal/site           Site registry, URL resolver, real site adapters
internal/pipeline       Text processing pipeline
internal/exporter       TXT/HTML/EPUB exporters
internal/store          Local staged raw storage and pipeline metadata
internal/state          CLI state such as language selection
internal/ui             Console prompting and output helpers
internal/model          Shared domain models
```

## Config

The project uses `data/settings.toml`, similar to `novel-downloader`.

You can create it with:

```bash
go run ./cmd/novel-cli config init
```

The embedded sample config is stored at `internal/config/resources/settings.sample.toml`.

Current config sections:

- `[general]`
- `[general.output]`
- `[general.parser]`
- `[general.debug]`
- `[[general.processors]]`
- `[sites.<site>]`
- `[plugins]`

## Storage Model

Downloaded and processed books are saved in staged form under `data/raw_data`:

```text
data/raw_data/<site>/<book_id>/book_info.<stage>.json
data/raw_data/<site>/<book_id>/chapters.<stage>.sqlite
data/raw_data/<site>/<book_id>/pipeline.json

Other generated files also live under `data/` for easier Docker volume mounting:

- `data/downloads/`
- `data/logs/`
- `data/novel_cache/`
- `data/go-novel-dl/state.json`
```

This keeps the same idea as the reference project: raw download first, then processors, then export from a chosen stage.

## Next Implementation Steps

1. Add interactive chapter-password prompting and cache management for ESJ protected posts
2. Expand `internal/site` with the next real site implementation
3. Add richer EPUB metadata, cover assets, and inline images
4. Add per-chapter retry policy and resumable partial refresh controls
5. Add Web API and Web UI on top of `internal/app`
