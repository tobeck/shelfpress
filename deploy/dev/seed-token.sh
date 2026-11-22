#!/usr/bin/env bash
#
# Create a BookStack API token without a browser, and print it as shell
# assignments.
#
# BookStack only issues API tokens through a logged-in user's profile page.
# CI has no human to click that form, so the token is created through the
# application's own models via `php artisan tinker` — which keeps the secret
# hashing and the api_tokens schema BookStack's problem, not ours.
#
# Usage:
#   eval "$(deploy/dev/seed-token.sh)"              # export into this shell
#   deploy/dev/seed-token.sh >> "$GITHUB_ENV"       # hand to later CI steps
#
# Environment:
#   COMPOSE_FILE    compose file to use (default: deploy/dev/docker-compose.yml)
#   SERVICE         compose service running BookStack (default: bookstack)
#   BOOKSTACK_URL   printed as BOOKSTACK_URL (default: http://localhost:8080)
#   TOKEN_NAME      name recorded on the token (default: shelfpress-dev)
#   ADMIN_EMAIL     user the token belongs to (default: admin@admin.com)
#   WAIT_TIMEOUT    seconds to wait for BookStack to be ready (default: 180)

set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-deploy/dev/docker-compose.yml}"
SERVICE="${SERVICE:-bookstack}"
BOOKSTACK_URL="${BOOKSTACK_URL:-http://localhost:8080}"
TOKEN_NAME="${TOKEN_NAME:-shelfpress-dev}"
ADMIN_EMAIL="${ADMIN_EMAIL:-admin@admin.com}"
WAIT_TIMEOUT="${WAIT_TIMEOUT:-180}"

# Progress and errors go to stderr so stdout stays clean for `eval` and for
# redirection into a CI env file.
log() { printf '%s\n' "$*" >&2; }
die() { printf 'seed-token: %s\n' "$*" >&2; exit 1; }

compose() { docker compose -f "$COMPOSE_FILE" "$@"; }

[ -f "$COMPOSE_FILE" ] || die "compose file not found: $COMPOSE_FILE (run from the repository root, or set COMPOSE_FILE)"

command -v docker >/dev/null 2>&1 || die "docker is not on PATH"

compose ps --status running --services 2>/dev/null | grep -qx "$SERVICE" \
  || die "compose service '$SERVICE' is not running — start it with 'make dev-up'"

# Wait for the application, not just the container. /status reports 200 only
# once the database migrations have run, so a token created before this would
# hit a half-migrated schema.
log "waiting for BookStack to be ready (up to ${WAIT_TIMEOUT}s)..."
deadline=$(( $(date +%s) + WAIT_TIMEOUT ))
until compose exec -T "$SERVICE" curl -fsS "http://localhost:80/status" >/dev/null 2>&1; do
  if [ "$(date +%s)" -ge "$deadline" ]; then
    die "BookStack did not become ready within ${WAIT_TIMEOUT}s.
  Tried: GET /status inside service '$SERVICE'.
  Check 'make dev-logs' — a failing database connection or an unrun
  migration is the usual cause."
  fi
  sleep 2
done

# Replicates BookStack's own UserApiTokenController::store — a 32-character
# secret stored as a hash, a 32-character token_id, and the model's default
# expiry. Re-running deletes any token of the same name first, so the script
# is idempotent rather than failing on the second run. Deleting by name only
# ever touches tokens this script created.
# shellcheck disable=SC2016  # single quotes are deliberate: the $-prefixed
# names below are PHP variables and must reach tinker unexpanded. Parameters
# are passed as container environment variables above, not interpolated, so a
# value containing a quote cannot break out of the snippet.
output=$(compose exec -T -w /app/www \
  -e SEED_ADMIN_EMAIL="$ADMIN_EMAIL" \
  -e SEED_TOKEN_NAME="$TOKEN_NAME" \
  "$SERVICE" php artisan tinker --execute '
use BookStack\Api\ApiToken;
use BookStack\Users\Models\User;
use Illuminate\Support\Facades\Hash;
use Illuminate\Support\Str;

$email = getenv("SEED_ADMIN_EMAIL");
$name  = getenv("SEED_TOKEN_NAME");

$user = User::query()->where("email", "=", $email)->first();
if ($user === null) {
    // Reported through stdout rather than exit(1): tinker turns a non-zero
    // exit into a BreakException stack trace that buries the actual cause.
    echo "NOUSER " . $email . "\n";
    return;
}

ApiToken::query()->where("name", "=", $name)->delete();

$secret  = Str::random(32);
$tokenId = Str::random(32);
while (ApiToken::query()->where("token_id", "=", $tokenId)->exists()) {
    $tokenId = Str::random(32);
}

(new ApiToken())->forceFill([
    "name"       => $name,
    "token_id"   => $tokenId,
    "secret"     => Hash::make($secret),
    "user_id"    => $user->id,
    "expires_at" => ApiToken::defaultExpiry(),
])->save();

echo "SEEDED " . $tokenId . " " . $secret . "\n";
' -- 2>&1) || die "artisan tinker failed:
$output"

if printf '%s\n' "$output" | grep -q '^NOUSER '; then
  die "no user with email '$ADMIN_EMAIL' exists in BookStack.
  On a fresh instance the admin is admin@admin.com; set ADMIN_EMAIL to
  match an existing user, or run 'make dev-clean && make dev-up' to reset."
fi

# The secret is hashed at rest, so this line is the only chance to read it.
line=$(printf '%s\n' "$output" | grep '^SEEDED ' | tail -1) \
  || die "could not find a token in the artisan output:
$output"

token_id=$(printf '%s\n' "$line" | awk '{print $2}')
secret=$(printf '%s\n' "$line" | awk '{print $3}')

[ -n "$token_id" ] && [ -n "$secret" ] || die "artisan produced an empty token:
$output"

log "created API token '$TOKEN_NAME' for $ADMIN_EMAIL"

printf 'BOOKSTACK_URL=%s\n' "$BOOKSTACK_URL"
printf 'BOOKSTACK_TOKEN_ID=%s\n' "$token_id"
printf 'BOOKSTACK_TOKEN_SECRET=%s\n' "$secret"
