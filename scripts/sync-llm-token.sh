#!/usr/bin/env bash
# =============================================================================
# sync-llm-token.sh — copy wrangler's (auto-refreshed) OAuth token into .env
# as AI_GATEWAY_API_KEY and recreate the backend container.
#
# STOPGAP: the OAuth token expires (~1h) but wrangler refreshes it whenever
# any wrangler command runs. Run this script whenever the frontend shows
# "llm: unexpected status 401".
#
# PERMANENT FIX: create a STATIC Cloudflare API Token with "Workers AI"
# permissions (dash.cloudflare.com → My Profile → API Tokens) and put it in
# .env as AI_GATEWAY_API_KEY — then you never need this script again.
# =============================================================================
set -euo pipefail
cd "$(dirname "$0")/.."

# Any wrangler command triggers the OAuth refresh, if due.
bunx --bun wrangler whoami > /dev/null 2>&1 || true

CFG_CANDIDATES=(
  "$HOME/Library/Preferences/.wrangler/config/default.toml"
  "$HOME/.wrangler/config/default.toml"
  "$HOME/.config/.wrangler/config/default.toml"
)
CFG=""
for p in "${CFG_CANDIDATES[@]}"; do
  [[ -f "$p" ]] && CFG="$p" && break
done
[[ -n "$CFG" ]] || { echo "ERROR: wrangler config not found (run: wrangler login)"; exit 1; }

TOKEN=$(sed -n 's/^oauth_token[[:space:]]*=[[:space:]]*"\(.*\)"$/\1/p' "$CFG" | head -n1)
[[ -n "$TOKEN" ]] || { echo "ERROR: no oauth_token in $CFG"; exit 1; }

ACCT=$(grep -E '^CLOUDFLARE_ACCOUNT_ID=' .env | head -n1 | cut -d= -f2 | tr -d '"')
CODE=$(curl -s -m 20 -o /dev/null -w '%{http_code}' \
  -H "Authorization: Bearer $TOKEN" \
  "https://api.cloudflare.com/client/v4/accounts/${ACCT}/ai/models/search?per_page=1" || true)
echo "Workers AI auth check → HTTP $CODE"
[[ "$CODE" == "200" ]] || { echo "ERROR: token cannot reach Workers AI"; exit 1; }

sed -i.bak "s|^AI_GATEWAY_API_KEY=.*|AI_GATEWAY_API_KEY=$TOKEN|" .env && rm -f .env.bak
echo ".env updated."

if command -v docker > /dev/null 2>&1; then
  docker compose up -d --force-recreate backend
  echo "Backend recreated. Done."
else
  echo "Restart your Go backend to pick up the new token."
fi