# Architecture

`go-novel-dl` follows the same core shape as `novel-downloader`, but expressed in idiomatic Go packages.

## Runtime Flow

```text
novel-dl command
  -> load and merge config
  -> build runtime
  -> resolve site adapter
  -> download raw book data
  -> persist raw stage
  -> run processors
  -> persist processed stage
  -> export to final formats
```

## Layers

### `internal/cli`

Thin Cobra commands. Responsible for:

- parsing flags and args
- interactive prompts when args are omitted
- turning command input into `model.BookRef`

### `internal/app`

Application orchestration layer. Responsible for:

- config-driven runtime assembly
- command-to-service flow
- coordinating storage, pipeline, export, and site registry

### `internal/config`

Typed configuration model plus a merge step:

- global defaults in `Config.General`
- site-level overrides in `Config.Sites`
- `ResolveSiteConfig(site)` to produce a merged runtime view
- runtime paths default into `data/` so the whole app can be mounted cleanly in Docker

### `internal/site`

Extension seam for downloader implementations.

Each site adapter supports:

- download
- download planning
- chapter fetch
- search
- URL resolution

The first full implementation is `esjzone`, including mirror-aware URL resolution for `esjzone.cc` and `esjzone.one`, authenticated login, cookie reuse, and protected chapter unlock hooks.

### `internal/store`

Owns staged local persistence.

Current stage files:

- `book_info.<stage>.json`
- `chapters.<stage>.sqlite`
- `pipeline.json`

Chapter content is stored in SQLite with GORM so later resumable and partial-refresh behavior can build on a stable persistence layer.

Download orchestration merges fresh chapter lists with stored raw-stage chapter data so already downloaded chapters are reused instead of being fetched again.

### `internal/pipeline`

Applies processors to downloaded chapter content. The initial scaffold ships with a simple `cleaner` processor.

### `internal/exporter`

Exports staged book content into user-facing files. Initial formats:

- `txt`
- `html`
- `epub`

## Why This Shape

This layout keeps command compatibility close to the reference project while making later extensions straightforward:

- real site implementations can replace starter adapters without changing novel-dl behavior
- web handlers can reuse `internal/app`
- pipeline/export/storage stay independent from novel-dl details
