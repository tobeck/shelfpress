# CLAUDE.md — shelfpress

> Publish a repo's markdown docs to BookStack. Any markdown tree; MADR
> decision records get first-class support.
> This file is the contract every coding agent obeys when working this repo.

Read [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) before starting any issue — it
holds the design decisions, and [docs/ROADMAP.md](docs/ROADMAP.md) holds the
milestone each issue belongs to.

## Stack

- Go 1.26 (module `github.com/tobeck/shelfpress`)
- CLI: `spf13/cobra`
- Config: `spf13/viper` + YAML
- Markdown: `yuin/goldmark` (structure only), `gopkg.in/yaml.v3` (frontmatter)
- HTTP: stdlib `net/http` — **no HTTP client library**
- Tests: stdlib `testing` + `net/http/httptest`
- Lint: `golangci-lint` (config in `.golangci.yml`)

## Commands (exact)

- Install deps: `go mod download`
- Build:        `make build`    (or `go build ./...`)
- Format:       `make fmt`      (`gofmt -w .`)
- Lint:         `make lint`     (`golangci-lint run`)
- Vet:          `go vet ./...`
- Unit tests:   `make test`     (`go test -race ./...`)
- Everything:   `make check`
- Integration:  `make test-integration` — **CI and local docker only.** Requires a
  live BookStack; do not attempt unless the issue is specifically about integration
  tests.

**Before finishing any issue, `make check` must pass.**

## Directory layout

- `cmd/shelfpress/` — `main()`, nothing else
- `internal/cli/` — cobra command tree, flag definitions
- `internal/config/` — config struct, file+env+flag loading, validation
- `internal/scan/` — walk the docs root, classify layout, build `Document`s
- `internal/document/` — frontmatter + title extraction, tag mapping
- `internal/bookstack/` — BookStack API client (shelves, books, pages, tags)
- `internal/sync/` — planner (diff) and applier; the `Plan` type
- `internal/report/` — human / json / markdown renderers, secret redaction
- `docs/` — architecture, roadmap, user docs
- `test/integration/` — build-tagged tests against a live BookStack
- `deploy/dev/` — docker-compose BookStack for local development
- `examples/ci/` — GitLab CI, Woodpecker, `docker run` examples

Dependency direction is one-way:
`cli → config, sync → bookstack, scan, document`. Nothing imports `cli`.
`bookstack` must not import `scan`, `document`, or `sync`.

## Code style

- Standard Go style; `gofmt` is enforced in CI. Run `make fmt` before finishing.
- Errors: wrap with `fmt.Errorf("doing x: %w", err)`. Never discard an error with
  `_`. Use `errors.Is`/`errors.As`, never string matching on error text.
- Exported identifiers get doc comments starting with the identifier name.
- Every package gets a package comment.
- Prefer small interfaces defined at the point of *use*, not beside the
  implementation. `bookstack.Client` is the testing seam — depend on the interface.
- Table-driven tests with subtests (`t.Run`) are the default.
- `context.Context` is the first parameter of anything doing I/O.
- No global mutable state. Config is passed explicitly.

## Hard design rules (these encode decisions already made)

1. **Never read `GITHUB_*` environment variables under `internal/`.** The core is
   CI-agnostic. Only `SHELFPRESS_*` and `BOOKSTACK_*` are read by the core; wrappers
   (`action.yml`) translate their platform's env into CLI flags. A test enforces
   this.
2. **Tag prefix is `sp-`** (`sp-source`, `sp-managed-by`, `sp-repo`). These are
   written into users' BookStack instances and are a compatibility surface — never
   rename them without an explicit migration issue.
3. **Only ever modify BookStack content tagged `sp-managed-by: shelfpress`.**
   Untagged content belongs to the user and must never be edited or deleted —
   report it, don't touch it.
4. **`plan` and `sync` share one code path.** `plan` runs the same planner and stops
   before applying. Never implement a separate "preview" path — a dry run that can
   diverge from the real run is worthless.
5. **Idempotency is a hard requirement.** A sync with no file changes must issue
   zero write requests. Emit an update op only when title, body, or tags actually
   differ.
6. **Secrets never reach logs or report output.** The reporter redacts tokens.
7. Send markdown to BookStack via the API's `markdown` field. Do not convert to
   HTML locally.

## Commit & PR conventions

- Conventional commits: `type(scope): summary`, e.g. `feat(scan): classify layouts`.
  Types: `feat`, `fix`, `docs`, `test`, `refactor`, `chore`, `ci`.
- Reference the issue in the PR body with `Closes #N`.
- One issue → one branch → one PR.
- **OSS-scaffolding issues (license, docs, templates, release process, security
  policy) must include a `## Rationale` section in the PR body** explaining why this
  approach and what alternatives were rejected. These PRs double as learning
  material for the maintainer.

## Never do this

- Don't edit files outside the issue's `Agent-Paths:` scope — raise a FOLLOWUP
  instead.
- Don't push, open PRs, or touch git remotes — the orchestrator does that.
- Don't add a dependency that isn't already in `go.mod` without saying so
  prominently in the PR body. Prefer the stdlib. This is a small tool; a large
  dependency tree is a maintenance burden on a solo maintainer.
- Don't commit real BookStack URLs, tokens, or any `.env` file.
- Don't implement two-way sync, attachment/image upload, or other publish targets —
  explicit non-goals in `docs/ARCHITECTURE.md`.
- **Don't build MADR linting or validation.** Checking that a record has the right
  sections, a valid status, or a coherent supersession graph belongs to `adrkit`,
  `adrlint` and `madr-lint`. shelfpress only warns about things that affect what
  reaches BookStack, and never blocks a publish.
- Don't use chapters. v0.1–v1.0 maps directories to books only; chapters are
  deliberately reserved for post-v1.0 nesting work.
- Don't widen a milestone. If an issue seems to need work from a later milestone,
  say so in the PR rather than pulling it forward.
