# Architecture — shelfpress

> **Publish your repo's markdown docs to BookStack.** Works with any markdown
> tree; [MADR](https://adr.github.io/madr/) architecture decision records get
> first-class support. A single binary, runnable from any CI.

Publish markdown documentation from a git repo to a
[BookStack](https://www.bookstackapp.com/) instance, so a repo that adds, edits,
or deletes a document gets its BookStack shelf updated automatically.

**The product is a single static binary, plus a container image wrapping it.** Any
CI system — GitHub Actions, GitLab CI, Woodpecker, Jenkins, Drone — or a plain cron
job can run it. The GitHub Action is a thin, optional wrapper over the same
container, never a privileged path.

## Goals

- One shelf per repo; books and pages derived from the file tree.
- Idempotent: re-running with no file changes makes no API writes.
- Stateless: no lockfile committed to the repo, no local database. BookStack itself
  holds the mapping.
- Deletions propagate (recoverable via BookStack's recycle bin).
- **CI-agnostic**: no CI platform's environment variables or conventions leak into
  the core. Wrappers adapt the environment to the CLI, not the reverse.
- Usable as a plain CLI or `docker run`, not only inside GitHub Actions.
- Distributed as prebuilt binaries (linux/darwin × amd64/arm64) and a multi-arch
  container image; buildable from source with `go build`.

## Scope

shelfpress is a **markdown → BookStack** sync tool. Any markdown tree works; no
frontmatter, heading, or naming convention is required.

**MADR gets first-class support** because ADRs-in-git is the use case that
motivated the tool: frontmatter becomes BookStack tags so decisions are
filterable by status, and the `NNNN-` filename prefix sets page order. None of it
is mandatory, and nothing about the sync path is MADR-shaped.

### Validation: publishing concerns only

shelfpress warns about things that affect **what reaches BookStack** — a document
with no title source, two pages that would share a title in one book, frontmatter
it does not recognise. Warnings never block a publish.

It is **not** a MADR validator and will not become one. Whether a record has the
right sections, a valid status, or a coherent supersession graph is answered well
by dedicated linters — [adrkit](https://github.com/mbeacom/adrkit),
[adrlint](https://github.com/polylint/adrlint),
[madr-lint](https://github.com/knktkc/madr-lint). They compose cleanly:

```sh
adrkit lint        # is the decision record correct?
shelfpress sync    # publish it
```

## Non-goals

Permanent, not merely deferred:

- **Linting or validating MADR correctness** — covered by the tools above.
- Two-way sync. BookStack is a publish target; the repo is the source of truth.
- Rendering BookStack-specific markup, attachments, or image upload.
- Supporting non-git sources.

## Why this exists

Surveyed November 2026. BookStack's core team has stated that
[git will not become first-class and will not be built in](https://github.com/BookStackApp/BookStack/issues/5361),
so the gap is permanent rather than something upstream will close. Existing
third-party tools top out around 10 GitHub stars:

| Tool | Language | Last release | Gap |
| ---- | -------- | ------------ | --- |
| [JaCoB1123/bookstack-import](https://github.com/JaCoB1123/bookstack-import) | Go | 2024-07 | Import only; no update path |
| [lazybytez/sync-markdown-bookstack-action](https://github.com/lazybytez/sync-markdown-bookstack-action) | JS | 2023-09 | Matches pages by H1 title, so renaming a heading orphans the page; no deletes |
| [barneycatatau/Bookstack-MD-Importer](https://github.com/barneycatatau/Bookstack-MD-Importer) | Python | 2026-06 | Interactive prompts, so unusable in CI; no deletes or dry run |
| [JulioLoayzaM/bsimport](https://github.com/JulioLoayzaM/bsimport) | Python | 2022-12 | Import only |

What shelfpress does differently: tag-based identity that survives renames,
deletion propagation, a trustworthy dry run, and no interactive prompts — it is
built for CI first.

Deferred items with a possible future are tracked in [ROADMAP.md](ROADMAP.md).

## Stack

| Concern        | Choice                        | Why |
| -------------- | ----------------------------- | --- |
| Language       | Go 1.26                       | Single static binary, tiny scratch container, strong stdlib HTTP. Requested. |
| CLI framework  | `spf13/cobra`                 | De facto Go CLI standard; subcommands (`sync`, `plan`, `version`). |
| Config         | `spf13/viper` + YAML          | File + env + flag precedence in one place. |
| Markdown       | `goldmark` + `yaml.v3`        | Goldmark for AST/heading extraction; yaml.v3 for MADR frontmatter. |
| HTTP           | stdlib `net/http`             | BookStack's API is small; a hand-rolled client stays dependency-light and testable. |
| Testing        | stdlib `testing` + `httptest` | Unit tests mock the API; integration tests hit a real local BookStack. |
| Lint           | `golangci-lint`               | Standard aggregate linter. |
| Container      | Distroless/scratch, multi-stage | Minimal attack surface; published to GHCR. |

Markdown is sent to BookStack as-is via the API's `markdown` field — no local
HTML conversion. Goldmark is used only to *read* structure (the H1 title), not to render.

## BookStack API facts this design relies on

Verified against <https://demo.bookstackapp.com/api/docs>:

- Auth is one header: `Authorization: Token <token_id>:<token_secret>`.
- `POST|PUT /api/pages` accept a **`markdown`** field directly, plus `name`,
  `book_id` / `chapter_id`, and `tags`.
- Hierarchy is **shelf → book → (chapter) → page**. A shelf cannot hold pages
  directly; a page must belong to a book or chapter.
- `PUT /api/shelves/{id}` takes a `books` array of book IDs — shelf membership is
  set from the shelf side, not the book side.
- List endpoints support `count` / `offset` paging and `filter[field]`.
- **List endpoints omit `tags`** (and page body) from their responses — see
  [BookStackApp/BookStack#2680](https://github.com/BookStackApp/BookStack/issues/2680).
  `/api/search` *does* return tags and supports tag terms (`[name=value]`) plus a
  type restriction (`{type:page}`), so shelfpress recovers its managed pages with a
  single search rather than fetching every page's detail. This is why the client
  exposes `SearchPagesByTag` alongside `ListPages`.
- `DELETE` sends content to BookStack's **recycle bin** (soft delete), so pruning
  is recoverable.

### Identity: how a page is matched to a file

The API addresses content by numeric ID only; it has no concept of a source path.
To reconcile across runs without local state, every object the tool creates is
tagged:

| Tag              | Value                                  | On |
| ---------------- | -------------------------------------- | -- |
| `sp-source`     | repo-relative path, e.g. `docs/decisions/0001-use-quarkus.md` | page |
| `sp-managed-by` | `shelfpress`                | shelf, book, page |
| `sp-repo`       | `owner/repo`                           | shelf, book, page |

A sync run lists objects carrying `sp-managed-by` + `sp-repo`, builds
`source path → object ID`, and diffs that against the files on disk. This gives
create / update / delete for free and makes the tool safe to point at a BookStack
that also holds hand-authored content: **anything untagged is never touched.**

## Mapping repo layout to BookStack

Rules applied under the configured ADR root (default `docs/decisions`):

**Subdirectory → book.** Each subdirectory becomes a book; each `.md` inside it
becomes a page in that book.

```
docs/decisions/
├── backend/
│   ├── 0001-use-quarkus.md
│   └── 0002-use-kafka.md
└── ui/
    └── 0001-use-vuejs.md

Shelf "Architecture Decisions"
├── Book "Backend"
│   ├── Page "Use Quarkus"
│   └── Page "Use Kafka"
└── Book "UI"
    └── Page "Use VueJS"
```

**Loose root file → its own book, holding one page.** A file directly under the
root becomes a book named from the ADR title, containing a single page with the
full ADR markdown.

```
docs/decisions/0001-use-quarkus.md

Shelf "Architecture Decisions"
└── Book "Use Quarkus"
    └── Page "Use Quarkus"
```

**Mixed trees apply both rules side by side** — subdirectories become books, loose
root files become their own books, all on the same shelf. No layout is an error.

Nested subdirectories deeper than one level are flattened into the top-level
subdirectory's book, with the relative path retained in the `sp-source` tag.
(Chapters are deliberately unused in v0.1; they are the natural future home for
deeper nesting.)

### Naming

- Page/book title: MADR frontmatter `title` if present, else the first H1, else the
  filename with the `NNNN-` numeric prefix stripped and dashes turned into spaces,
  title-cased.
- Book name for a subdirectory: directory name, dashes/underscores to spaces,
  title-cased.
- Page `priority` is set from the MADR `NNNN` prefix where present, so BookStack
  orders ADRs the way the repo numbers them.

### MADR frontmatter → page tags

Recognised frontmatter keys become BookStack tags, making decisions filterable in
BookStack's UI: `status`, `date`, `decision-makers`, `consulted`, `informed`.
List values are joined with `, `. Unknown keys are ignored. Frontmatter is stripped
from the body before upload.

## Reconciliation

A single pass, expressed as a **plan** then an **apply** — the same code path backs
both `plan` (dry run) and `sync`, which is what makes the dry run trustworthy.

1. **Scan** the ADR root; parse each `.md` into a `Document{SourcePath, Title, Body, Tags, Priority, Book}`.
2. **Fetch** current state: the shelf, and all books/pages tagged for this repo.
3. **Diff** into a `Plan` of ops: `CreateShelf`, `CreateBook`, `CreatePage`,
   `UpdatePage`, `DeletePage`, `DeleteBook`, `UpdateShelfBooks`.
   - Update is emitted only when title, body, or tags actually differ — this is what
     makes a no-change run write nothing.
4. **Apply** in dependency order: shelf → books → pages → shelf membership → prunes.

Pruning (default on, `prune: false` / `--no-prune` to disable) deletes pages whose
`sp-source` file is gone, then books left empty and managed by the tool. Deletes
are soft (recycle bin).

Idempotency check: body comparison normalises trailing whitespace and line endings,
because BookStack echoes back markdown it has stored.

**Search results carry no body.** `/api/search` returns tags but omits page
content, so the planner must fetch each managed page's detail with
`GET /api/pages/{id}` before comparing bodies. Comparing against the search
result instead makes every page look changed and rewrites the whole shelf on
every run — a bug that shipped through unit tests at 91% coverage because the
test double returned bodies the real API never sends. The fakes now strip body
content from search results, and `make test-integration` (v0.2) verifies the
behaviour against a live instance.

## Module layout

```
cmd/shelfpress/              main(); wires cobra commands
internal/config/             config struct, file+env+flag loading, validation
internal/scan/               walk the ADR root, classify layout, build Documents
internal/madr/               frontmatter + title extraction, tag mapping
internal/bookstack/          API client: shelves, books, pages, tags, paging, retry
internal/sync/               planner (diff) + applier; the Plan type
internal/report/             human / json / markdown renderers; secret redaction
docs/                        ARCHITECTURE.md and user docs
test/integration/            tests against a live local BookStack (build-tagged)
deploy/dev/                  docker-compose for local BookStack + seed script
action.yml                   GitHub Action wrapper (thin; flags only, no logic)
examples/ci/                 GitLab CI, Woodpecker, docker run examples
Dockerfile                   multi-stage build -> minimal runtime image
.goreleaser.yml              cross-compiled binaries for releases
```

Dependency direction is one-way: `sync` depends on `bookstack`, `scan`, `madr`;
nothing depends on `sync`. `bookstack` knows nothing about MADR or files, and no
package under `internal/` knows anything about GitHub.

## Interfaces (the seam that keeps this testable)

```go
// internal/bookstack
type Client interface {
    ListShelves(ctx context.Context, f Filter) ([]Shelf, error)
    CreateShelf(ctx context.Context, s ShelfInput) (Shelf, error)
    UpdateShelfBooks(ctx context.Context, id int, bookIDs []int) error
    ListBooks(ctx context.Context, f Filter) ([]Book, error)
    CreateBook(ctx context.Context, b BookInput) (Book, error)
    DeleteBook(ctx context.Context, id int) error
    ListPages(ctx context.Context, f Filter) ([]Page, error)
    CreatePage(ctx context.Context, p PageInput) (Page, error)
    UpdatePage(ctx context.Context, id int, p PageInput) (Page, error)
    DeletePage(ctx context.Context, id int) error
}
```

The planner takes a `Client` and is unit-tested against a fake; the real client is
tested against `httptest` and, in integration tests, a live BookStack.

## Configuration

Precedence: flags > env > config file > defaults. Secrets are env-only in practice.

```yaml
# .shelfpress.yml
root: docs/decisions           # directory to scan
shelf: Architecture Decisions  # shelf name (default: repo name)
shelf_description: "ADRs for owner/repo"
prune: true                    # delete BookStack content when source file is gone
dry_run: false
```

| Env | Flag | Meaning |
| --- | --- | --- |
| `BOOKSTACK_URL` | `--url` | base instance URL |
| `BOOKSTACK_TOKEN_ID` | `--token-id` | API token id |
| `BOOKSTACK_TOKEN_SECRET` | `--token-secret` | API token secret |
| `SHELFPRESS_REPO` | `--repo` | project identifier for the `sp-repo` tag and default shelf name |

The core reads **only** `SHELFPRESS_*` and `BOOKSTACK_*` variables — never `GITHUB_*` or any
other CI platform's variables. Wrappers map their platform's environment onto these
(the GitHub Action passes `GITHUB_REPOSITORY` as `--repo`; a GitLab job would pass
`CI_PROJECT_PATH`). If `--repo` is unset the tool falls back to parsing the git
remote, then to the directory name, so it also works outside CI entirely.

Secrets are never written to logs or report output; the reporter redacts them.

### Output and exit codes (the integration contract)

Because the binary must slot into arbitrary CI systems, output and exit codes are
part of the public interface and covered by semver:

- `--format human|json|markdown` — `human` for terminals, `json` for scripting,
  `markdown` for CI summaries.
- `--output-file PATH` — write the report to a file instead of stdout. A GitHub
  Action wrapper points this at `$GITHUB_STEP_SUMMARY`; other systems point it
  wherever they collect artifacts.
- Exit codes: `0` success / no changes; `1` runtime error; `2` config or validation
  error; `3` from `plan` only, meaning "changes are pending" (opt-in via
  `--detailed-exitcode`, mirroring `terraform plan`), so any CI can gate on drift.

## Running it

### Plain binary

```sh
shelfpress sync --root docs/decisions --url https://wiki.example.com
```

### Container (any CI, or locally)

```sh
docker run --rm -v "$PWD:/work" -w /work \
  -e BOOKSTACK_URL -e BOOKSTACK_TOKEN_ID -e BOOKSTACK_TOKEN_SECRET \
  ghcr.io/tobeck/shelfpress:v1 sync --root docs/decisions
```

### GitHub Action (one wrapper among several)

Docker-based action (`action.yml`) referencing the published GHCR image, so
consumers pay no build cost. It only translates inputs into CLI flags — it holds no
logic of its own. Inputs mirror the config keys; `bookstack-url`, `token-id`,
`token-secret` are required.

```yaml
- uses: tobeck/shelfpress@v1
  with:
    bookstack-url: ${{ secrets.BOOKSTACK_URL }}
    token-id: ${{ secrets.BOOKSTACK_TOKEN_ID }}
    token-secret: ${{ secrets.BOOKSTACK_TOKEN_SECRET }}
    root: docs/decisions
```

On `pull_request` the wrapper runs `plan` and points `--output-file` at
`$GITHUB_STEP_SUMMARY`, so a reviewer sees exactly what a merge would publish. The
same `plan --format markdown` output works in any CI that can display a file:

```
## shelfpress plan
Shelf: Architecture Decisions (exists)

| Action | Type | Target |
|--------|------|--------|
| create | page | Use Kafka (book: Backend) |
| update | page | Use Quarkus |
| delete | page | Use Redis (source removed) |

3 changes. Dry run — nothing was written.
```

### Other CI systems

Documented examples ship for GitLab CI and Woodpecker, both invoking the same
container. Nothing about the tool privileges GitHub; a `docker run` in any runner is
the baseline supported path, and building from source with `go build` requires no
published image at all.

## Local development environment

`deploy/dev/docker-compose.yml` brings up BookStack + MariaDB
(`lscr.io/linuxserver/bookstack`, `linuxserver/mariadb`) for manual testing and
integration tests.

**Known wrinkle:** BookStack API tokens are normally created through the web UI.
Automated integration tests need them seeded non-interactively, so the dev setup
provides a seed step that inserts a known token directly via BookStack's CLI/DB
rather than expecting a human to click through. Integration tests are behind a
`//go:build integration` tag and skip cleanly when `BOOKSTACK_URL` is unset, so
`go test ./...` stays fast and hermetic for contributors without Docker.

## CI/CD

- **PR**: lint, `go vet`, unit tests with race detector, build; integration tests
  against a composed BookStack service.
- **main**: build and push multi-arch (amd64/arm64) image to GHCR as `:edge`.
- **tag `v*`**: push `:vX.Y.Z`, `:vX.Y`, `:latest`; attach binaries to a GitHub
  Release via GoReleaser; move the floating `v1` tag for Action consumers.

## Build order

Release-by-release sequencing, and the maintenance practice paired with each
milestone, live in [ROADMAP.md](ROADMAP.md). In dependency terms:

1. Repo scaffold, CLI skeleton, CI, config loading.
2. `madr` parser and `scan` walker (pure, no network — heavily unit-testable).
3. `bookstack` client (shelves, books, pages, tags, paging, retry).
4. `sync` planner, then applier.
5. Reporting (human/json/markdown), dry run wiring.
6. Dockerfile, `action.yml`, CI examples, GHCR publishing.
7. Local BookStack dev env + headless token seeding + integration tests.
8. User-facing docs, stability policy.

## Project hosting

**GitHub, primary, for now** — revisit before v1.0.

Upstream BookStack [migrated to Codeberg in April 2026](https://www.bookstackapp.com/blog/project-migrated-to-codeberg/)
(GitHub retained as a mirror), citing Git-LFS abuse limits, GitHub's AI-tooling
focus, and preference for a non-profit forge. Those reasons apply weakly here: a Go
CLI has no LFS assets, and the remaining two are values questions rather than
constraints.

What GitHub supplies that Codeberg currently does not: Actions CI with service
containers (needed for the ephemeral-BookStack integration tests), GHCR, arm64
runners for multi-arch images, and the Action marketplace listing. Codeberg's
Woodpecker CI is gated behind volunteer approval, shares capacity, and is
linux/amd64 only.

Crucially, the binary-and-container-first design above means **this choice does not
affect users** — they run an image or a binary, not a forge. It is therefore cheap to
revisit, and a CI-pushed read-only Codeberg mirror can be added at any time to stay
visible in BookStack's community.

## Risks

| Risk | Mitigation |
| ---- | ---------- |
| Tag-based identity breaks if a user edits tags in BookStack | Documented as managed content; orphan report rather than silent re-create. |
| Pruning deletes more than intended | Only touches `sp-managed-by`-tagged content; dry run by default on PRs; soft delete. |
| BookStack API version drift | Client pinned to documented fields; integration tests catch breakage. |
| Title collisions (two ADRs, same title) | Identity is the source-path tag, not the title; duplicates are allowed and warned about. |
| Rate limiting on large repos | Client honours `429`/`Retry-After` with bounded exponential backoff. |
| CI-platform lock-in creeping back in | Core reads only `SHELFPRESS_*`/`BOOKSTACK_*`; a lint/test check asserts no `GITHUB_*` reference exists under `internal/`. |
