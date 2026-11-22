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

shelfpress authenticates with a BookStack API token, which BookStack normally
issues through a logged-in user's profile page. Getting one headlessly — for CI,
where there is no human to click — is handled separately; see issue #25.

To create one by hand in the meantime: sign in, go to **Edit Profile → API
Tokens → Create Token**, and export the pair.

```sh
export BOOKSTACK_URL=http://localhost:8080
export BOOKSTACK_TOKEN_ID=...
export BOOKSTACK_TOKEN_SECRET=...
```

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
