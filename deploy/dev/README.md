# Local BookStack for development

A throwaway BookStack instance to develop and test shelfpress against. The
integration suite ([`test/integration/`](../../test/integration/)) runs against
this.

> ⚠️ **Development only.** The credentials below are fixed, public, and
> committed to this repository, and the encryption key is shared by everyone who
> clones it. The services bind to `127.0.0.1` so they are not reachable from your
> network — **do not change that**, do not put this behind a reverse proxy or
> expose the port, and never point it at data you care about.

## Usage

```sh
make dev-up      # start, and wait until the API actually answers
make dev-token   # create an API token and print it
make dev-down    # stop, keeping the database
make dev-clean   # stop and delete the database — back to a known-empty instance
make dev-logs    # follow the logs
```

`make dev-up` does not return until both containers report healthy, so a test
run started immediately afterwards will not race BookStack's first-boot
migrations.

First start takes a couple of minutes: BookStack runs its database migrations
before serving anything. Later starts are quick.

## What you get

| | |
| --- | --- |
| URL | <http://localhost:8080> |
| Login | `admin@admin.com` / `password` |
| BookStack | `lscr.io/linuxserver/bookstack:26.09.3` |
| Database | `mariadb:11.4.13` |

The login is BookStack's own default for a fresh install, not something this
compose file sets. Change it from the UI if you like; `make dev-clean` resets it.

Image tags are pinned on purpose. An integration suite whose dependencies drift
underneath it fails for reasons unrelated to the change being tested — which is
the failure mode this whole milestone exists to eliminate.

## API tokens

shelfpress authenticates with a BookStack API token. BookStack only issues these
through a logged-in user's profile page (**Edit Profile → API Tokens → Create
Token**), which is fine by hand but useless in CI, where there is no browser and
no human to click it. `seed-token.sh` creates one headlessly instead:

```sh
make dev-token                              # print a token
eval "$(deploy/dev/seed-token.sh)"          # export it into this shell
deploy/dev/seed-token.sh >> "$GITHUB_ENV"   # hand it to later CI steps
```

It prints exactly three lines on stdout, so it can be `eval`'d or redirected
into a CI environment file. Progress and errors go to stderr and stay out of the
way:

```
BOOKSTACK_URL=http://localhost:8080
BOOKSTACK_TOKEN_ID=...
BOOKSTACK_TOKEN_SECRET=...
```

**What the token can do.** It belongs to the default admin, so it has full API
access to all content — enough for the integration suite to create and delete
shelves, books and pages. The expiry is BookStack's default of 100 years.

**Re-running is safe.** Each run deletes any token with the same name and makes a
new one, so it never trips a unique constraint. The secret is hashed at rest and
only visible at creation, so each run necessarily issues a fresh pair; a token
from an earlier run stops working. Re-export after re-running.

**It waits for BookStack** to answer `/status` rather than assuming it is up, so
it is safe to run immediately after starting the containers. If BookStack never
becomes ready it fails with what it tried; `WAIT_TIMEOUT` (default 180s) adjusts
the budget.

`ADMIN_EMAIL`, `TOKEN_NAME`, `BOOKSTACK_URL`, `SERVICE` and `COMPOSE_FILE` are
all overridable — see the comments at the top of the script.

### How it works, and why

The token row is created through BookStack's own models, with
`php artisan tinker` inside the container, replicating what
`UserApiTokenController::store` does: a 32-character secret stored as a hash, a
32-character `token_id`, and the model's default expiry.

The alternative — writing the row into MariaDB with SQL — would mean generating
BookStack's bcrypt secret hash ourselves and encoding the `api_tokens` schema
here. Both are BookStack's implementation details, and both would break silently
on an upgrade, leaving a token that exists but never authenticates. Going through
artisan keeps them BookStack's problem.

Keep real tokens out of this repository. There is no `.env` file here and none
should be committed — `.env` is already in `.gitignore`.

## Troubleshooting

**`make dev-up` times out.** Give it longer on a first run (migrations), then
check `make dev-logs`. The healthcheck polls BookStack's `/status` endpoint,
which reports 200 only once the database, cache, and session all work.

**Port 8080 is taken.** Change both the published port and `APP_URL` in
`docker-compose.yml` — they have to agree, or BookStack generates links and API
URLs pointing at the wrong place.

**Starting from scratch.** `make dev-clean` removes both volumes. Anything you
created in BookStack is gone, which is usually what you want between test runs.
