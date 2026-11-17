---
# Sortie workflow for shelfpress — start with: sortie /path/to/WORKFLOW.md
tracker:
  kind: github
  api_key: $SORTIE_GITHUB_TOKEN          # PAT: Issues RW, Contents RW, PRs RW, Metadata R
  project: tobeck/shelfpress
  query_filter: "label:agent"
  active_states: [agent:todo, agent:doing]
  in_progress_state: agent:doing
  handoff_state: agent:review
  terminal_states: [agent:done]

agent:
  kind: claude-code
  command: claude
  max_concurrent_agents: 1
  max_sessions: 3
  max_turns: 4

claude-code:
  permission_mode: bypassPermissions
  model: sonnet
  effort: medium
  max_turns: 60
  max_budget_usd: 3

polling:
  interval_ms: 60000

workspace:
  root: /var/lib/sortie/workspaces/shelfpress
db_path: /var/lib/sortie/shelfpress.db

hooks:
  after_create: |
    git clone https://github.com/tobeck/shelfpress.git .
    go mod download
  before_run: |
    git fetch origin main
    git checkout -B "sortie/issue-$SORTIE_ISSUE_IDENTIFIER" origin/main
    go mod download
  after_run: |
    set -e
    if [ "$(git rev-list --count origin/main..HEAD)" -gt 0 ]; then
      git push -u origin "sortie/issue-$SORTIE_ISSUE_IDENTIFIER"
      gh pr create --base main --head "sortie/issue-$SORTIE_ISSUE_IDENTIFIER" \
        --title "sortie #$SORTIE_ISSUE_IDENTIFIER" --body "Closes #$SORTIE_ISSUE_IDENTIFIER" \
        2>/dev/null || echo "PR already exists"
      gh issue edit "$SORTIE_ISSUE_IDENTIFIER" --remove-label agent 2>/dev/null || true
    fi
---
You are a senior Go engineer working a single issue to completion in an isolated
checkout on branch `sortie/issue-{{ .issue.identifier }}`.

## Issue #{{ .issue.identifier }}: {{ .issue.title }}
{{ .issue.description }}

Read `CLAUDE.md` first — it is the contract, and its "Hard design rules" section
encodes decisions that are already settled. `docs/ARCHITECTURE.md` has the design
detail and `docs/ROADMAP.md` says which milestone this issue belongs to.

Implement the issue, add or update tests, and make `make check` pass (it runs
gofmt, golangci-lint, race-enabled tests, and the build). Commit referencing
#{{ .issue.identifier }} using conventional-commit format.

Stay strictly within the issue's `Agent-Paths`. Do not push or open PRs — the hooks
do that. Do not pull work forward from a later milestone: if the issue seems to
need it, note that instead of implementing it.

Raise out-of-scope problems as `FOLLOWUP: <title> :: <detail>`. If blocked, prefix
your final message with `BLOCKED:`.
