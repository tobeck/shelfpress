# Roadmap — v0.1 to v1.0

This project has two deliverables running in parallel: **a working tool**, and
**a maintained open-source project**. The second is a learning goal, so the
milestones below deliberately pair each release with one maintenance practice.
You practise cutting a release six times before v1.0 rather than once.

Guiding rule: **every milestone ends in a tagged release that a stranger could
install and use.** No milestone is "internal refactoring only" — if there's nothing
a user would notice, it belongs inside another milestone.

See [ARCHITECTURE.md](ARCHITECTURE.md) for the design these milestones implement.

## Why staged, not one big v1.0

A single large v0.1 would front-load every decision before any feedback arrives, and
would skip the part you want to learn: the maintenance loop. Staging means the first
real ADR reaches BookStack early, and each later release exercises a different
maintainer muscle — versioning, triage, reviewing an outside PR, deprecating a flag,
handling a security report.

### Revised after v0.1 (2026-11-19)

The original plan put integration testing at v0.5, after the destructive
features. Wiring up the CLI in #22 proved that wrong: **every sync was rewriting
every page**, because BookStack's search endpoint returns tags but no body
content and the planner compared against the empty string. Unit tests at 91%
coverage passed throughout — the fake returned bodies the real API never sends,
so fakes agreeing with each other proved nothing.

Two consequences, both now reflected below:

1. **Integration testing moves to v0.2**, ahead of everything else. The next bug
   of that shape is equally invisible to fakes.
2. **Pruning moves behind it.** Deleting content is the one irreversible thing
   shelfpress does; it must not ship before the tool has been proven against a
   real BookStack.

The first real user is already waiting (manual/scripted syncing today), which
also argues for proving correctness before adding surface area.

---

## v0.1 — Walking skeleton

**Capability:** the smallest thing that genuinely publishes. Flat markdown files in
one directory become a book of pages on a shelf. Create and update only — no
deletion, no subdirectories, no container.

**Maintenance practice:** the first release. Pick the license, write a README that
gets someone running in under a minute, cut `v0.1.0` by hand.

Scope:
- `scan` walks one flat directory; `madr` extracts title and body
- `bookstack` client: shelves, books, pages, tags, auth, paging
- `sync` planner + applier for create/update
- `plan` (dry run) and `sync` commands; `--format human`
- Tag-based identity (`sp-source`, `sp-managed-by`, `sp-repo`)
- `LICENSE` (MIT), `README.md`, `go.mod`, CI running lint + unit tests
- Prebuilt binaries attached to the release

Done when: you can point it at a real repo and see ADRs appear in BookStack, and
re-running writes nothing.

**You learn:** semver from zero, what belongs in a first README, release mechanics.

---

## v0.2 — Proof against a real BookStack

**Capability:** no new user-facing features, and that is the point. A local
BookStack via docker-compose, plus integration tests running against it in CI
with no secrets, so fork PRs get full coverage.

**Why first:** #22 found that every sync rewrote every page, invisible to unit
tests because the fake returned body content the real API omits. Everything after
this milestone is built on an assumption this milestone verifies.

**Maintenance practice:** `SECURITY.md` and branch protection. The tool handles
API tokens, so a vulnerability-report process is not theoretical.

Scope:
- `deploy/dev/docker-compose.yml` — BookStack + MariaDB
- **Headless API-token seeding** (BookStack normally issues tokens through its web
  UI; tests need them seeded via CLI/DB). The known hard part of the milestone.
- `test/integration/` behind `//go:build integration`, skipping when
  `BOOKSTACK_URL` is unset so `go test ./...` stays hermetic
- CI job with BookStack as a service container — no secrets, so forks are covered
- **An idempotency test against the real API**: sync, sync again, assert zero
  writes. This is the regression that motivated the reorder.
- Round-trip assertions: what BookStack stores and returns for markdown, tags
  and priority, documented as findings in `docs/ARCHITECTURE.md`
- `SECURITY.md`, branch protection, `CODEOWNERS`

Done when: `make dev-up && make test-integration` works from a clean clone, a
fork PR runs the full suite, and the idempotency test passes against a real
BookStack.

**You learn:** CI that works for contributors without handing them secrets; why
a test double that is kinder than reality is worse than no test; security
disclosure process.

---

## v0.3 — Full mapping and deletion

**Capability:** the complete layout rules — subdirectory→book, loose file→its own
book+page, mixed trees applying both. Pruning when a source file disappears,
`--no-prune` to disable.

**Why after integration tests:** pruning is the only irreversible thing
shelfpress does. It must not ship before the tool has been proven against a real
instance — and the integration suite from v0.2 is what proves a delete removes
what it should and nothing else.

**Maintenance practice:** `CHANGELOG.md` (Keep a Changelog format) and issue/PR
templates. First release with existing users, so breaking changes now have a
cost — write the changelog entry that explains the mapping change.

Scope:
- Layout classification and the three mapping rules
- `DeletePage` / `DeleteBook` ops, prune config and flag
- Orphan reporting for untagged content (never touched, only reported)
- **Integration tests for deletion**: a pruned page really goes to the recycle
  bin, and untagged content survives a prune
- `CHANGELOG.md`, `.github/ISSUE_TEMPLATE/`, PR template

Done when: deleting a file removes its page from BookStack, a mixed-layout repo
syncs correctly, and both are verified against a live instance.

**You learn:** writing a changelog someone else can read; migration notes; why
default-on destructive behaviour needs loud documentation.

---

## v0.4 — Container and CI-agnostic distribution

**Capability:** multi-arch container image on GHCR, plus the GitHub Action wrapper
and worked examples for GitLab CI and Woodpecker. `--format json`, `--output-file`,
and the documented exit codes land here, since they are the integration contract.

**Maintenance practice:** automated release pipeline (GoReleaser + GHCR publish on
tag) and Dependabot. The release stops being hand-rolled.

Scope:
- `Dockerfile` (multi-stage, minimal runtime), multi-arch amd64/arm64
- `action.yml` — thin wrapper, flags only, no logic
- `examples/ci/` for GitLab CI, Woodpecker, plain `docker run`
- `--format json|markdown`, `--output-file`, `--detailed-exitcode`
- `.goreleaser.yml`, release workflow, `dependabot.yml`
- A test asserting no `GITHUB_*` reference exists under `internal/`

Done when: `docker run ghcr.io/...` syncs a repo, and the same image works unchanged
in a non-GitHub CI.

**You learn:** release automation, supply-chain basics, multi-arch builds, the
floating-major-tag convention (`v1`) that Action consumers depend on.

---

## v0.5 — Metadata and review workflow

**Capability:** MADR frontmatter → BookStack tags (`status`, `date`,
`decision-makers`, `consulted`, `informed`), so decisions are filterable in
BookStack. Plan output rendered as a CI summary for pull-request review.

**Maintenance practice:** `CONTRIBUTING.md` and a `good first issue` triage pass.
Make the project contributable by someone who isn't you.

Scope:
- Frontmatter parsing, tag mapping, body stripping
- `priority` from the `NNNN` filename prefix
- **Publish-time warnings**: no title source, duplicate titles within a book,
  unrecognised frontmatter. Warnings only — shelfpress never validates MADR
  correctness, which `adrkit`/`adrlint`/`madr-lint` already do well.
- Markdown plan rendering wired to PR review in the Action example
- `CONTRIBUTING.md` (dev setup, test commands, PR expectations), `CODE_OF_CONDUCT.md`
- Label taxonomy; a few genuinely small issues marked `good first issue`
- **Considered: automated Claude PR review**
  ([claude-code-action](https://github.com/anthropics/claude-code-action)).
  Deliberately deferred from v0.1 — while the maintainer writes and reviews every
  PR, an agent reviewing agent-written code shares its blind spots. It earns its
  place once outside contributions arrive. If adopted: use the `pull_request`
  trigger, never `pull_request_target` (GitHub withholds secrets from forks on the
  former, and the action restores `CLAUDE.md` from the base branch so a fork
  cannot rewrite the reviewer's own instructions — which matters for a tool whose
  input is untrusted markdown). Authenticate with a subscription OAuth token
  rather than an API key to avoid unbounded spend on a public repo.

Done when: an outside contributor could clone, build, test, and submit a PR using
only the docs.

**You learn:** writing docs for contributors rather than users; triage; how labels
shape what help you get.

---

## v1.0 — Stability commitment

**Capability:** no new features. Hardening, polish, and an explicit statement of
what is now stable: the CLI flags, the config file keys, the tag schema, the exit
codes, and the output formats.

**Maintenance practice:** the stability promise itself. Documenting what you *won't*
break is the most consequential maintainer act, and the one HashiCorp got wrong.

Scope:
- Error messages reviewed for actionability
- Rate-limit/retry behaviour verified against a real instance
- README with real screenshots of the BookStack result
- `docs/` for configuration reference and troubleshooting
- **Compatibility policy**: what semver covers, how the `v1` tag moves, deprecation
  period for flags
- **`GOVERNANCE.md`**: MIT, and a written commitment not to relicense — the
  HashiCorp lesson is about reversing expectations, not about license terms
- Announce where BookStack's community actually is (Codeberg, Discord, subreddit),
  not only a GitHub release

Done when: the public interface is documented as stable and you'd be comfortable
telling a stranger to depend on it.

**You learn:** what a stability guarantee costs; how to say no to scope creep once
people depend on you.

---

## Deliberately after v1.0

Parked, not forgotten. Keeping these out is what makes v1.0 reachable.

- **Chapters** for directory nesting deeper than one level (v0.1–v1.0 flattens into
  the top-level book)
- **Two-way sync** — explicitly a non-goal; BookStack is a publish target
- Image and attachment upload
- Non-git sources
- Other publish targets (Confluence, Outline)
- Codeberg mirror or migration — revisit with real information at v1.0

## Build order summary

```
v0.1  scaffold -> madr/scan -> bookstack client -> planner -> applier -> release
v0.2  dev compose -> headless token seeding -> integration tests
      -> real-API idempotency proof -> security policy
v0.3  layout rules -> prune (+ integration tests for deletion)
      -> changelog + templates
v0.4  container -> action + CI examples -> output formats -> release automation
v0.5  frontmatter tags -> publish-time warnings -> CI summary
      -> contributor docs
v1.0  hardening -> docs -> stability + governance policy
```

Dependencies are strictly sequential between milestones; within a milestone, issues
are scoped to be path-disjoint so they can be worked in parallel.
