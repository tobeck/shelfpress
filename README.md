# shelfpress

**Publish your repo's markdown docs to [BookStack](https://www.bookstackapp.com/).**
Built for [MADR](https://adr.github.io/madr/) architecture decision records; works
with any markdown tree. A single binary, runnable from any CI.

> ⚠️ **Early development.** Working toward [v0.1](docs/ROADMAP.md). Not usable yet.

## What it does

Keep your ADRs in git, where they're reviewed alongside the code they describe, and
have them show up in BookStack for everyone else. Add, edit, or delete a markdown
file; shelfpress makes BookStack match on your next CI run.

```
docs/decisions/                      Shelf "Architecture Decisions"
├── backend/                         ├── Book "Backend"
│   ├── 0001-use-quarkus.md    ──>   │   ├── Page "Use Quarkus"
│   └── 0002-use-kafka.md            │   └── Page "Use Kafka"
└── ui/                              └── Book "UI"
    └── 0001-use-vuejs.md                └── Page "Use VueJS"
```

- **Idempotent** — no file changes means no API writes.
- **Stateless** — no lockfile in your repo, no database. Content is matched by tags
  written into BookStack itself.
- **Safe** — only touches content it created. Hand-authored pages are never
  modified, and deletes go to BookStack's recycle bin.
- **CI-agnostic** — a binary and a container. GitHub Actions, GitLab CI, Woodpecker,
  a cron job; nothing is GitHub-specific.

## Usage

```sh
shelfpress plan --root docs/decisions    # preview changes
shelfpress sync --root docs/decisions    # apply them
```

Or with the container:

```sh
docker run --rm -v "$PWD:/work" -w /work \
  -e BOOKSTACK_URL -e BOOKSTACK_TOKEN_ID -e BOOKSTACK_TOKEN_SECRET \
  ghcr.io/tobeck/shelfpress:v1 sync --root docs/decisions
```

### GitHub Actions

```yaml
- uses: tobeck/shelfpress@v1
  with:
    bookstack-url: ${{ secrets.BOOKSTACK_URL }}
    token-id: ${{ secrets.BOOKSTACK_TOKEN_ID }}
    token-secret: ${{ secrets.BOOKSTACK_TOKEN_SECRET }}
    root: docs/decisions
```

Examples for other CI systems will live in [`examples/ci/`](examples/ci/).

## Configuration

Precedence: flags > environment > config file > defaults.

```yaml
# .shelfpress.yml
root: docs/decisions
shelf: Architecture Decisions
prune: true          # remove BookStack content when its source file is deleted
```

| Environment variable | Purpose |
| --- | --- |
| `BOOKSTACK_URL` | BookStack instance base URL |
| `BOOKSTACK_TOKEN_ID` | API token ID |
| `BOOKSTACK_TOKEN_SECRET` | API token secret |

Create an API token in BookStack under your user profile → **API Tokens**
(requires the *Access System API* permission).

## Documentation

- [Architecture](docs/ARCHITECTURE.md) — design, mapping rules, how identity works
- [Roadmap](docs/ROADMAP.md) — path from v0.1 to v1.0

## Development

```sh
make check    # format, lint, test, build
```

Requires Go 1.26.

## License

[MIT](LICENSE) — the same license as BookStack itself.

This project is MIT licensed and will stay that way. Relicensing would require the
consent of every contributor, and the maintainer commits to not pursuing it.

## Related

shelfpress is an independent project and is not affiliated with BookStack. BookStack
is developed at [codeberg.org/bookstack/bookstack](https://codeberg.org/bookstack/bookstack).
