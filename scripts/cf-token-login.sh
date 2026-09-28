#!/usr/bin/env bash
# =============================================================================
# cf-token-login.sh
#
# Wire a Cloudflare API Token into the local Go backend so it calls
# **Workers AI directly** — no AI Gateway required, and never needs an API
# Gateway URL.
#
# IMPORTANT: the wrangler OAuth token from `wrangler login` does NOT
# authenticate the Workers AI REST API (it 401s). You must create a dedicated
# Cloudflare **API Token** with the "Workers AI → Edit" permission:
#
#   Dashboard → My Profile → API Tokens → Create Token
#   → Template "Workers AI: Read and Write" (or custom with Workers AI:Edit)
#
#   https://dash.cloudflare.com/profile/api-tokens
#
# This script:
#  1. Reads the API token from $1 / CLOUDFLARE_API_TOKEN.
#  2. Validates it against the Workers AI chat-completions endpoint.
#  3. Writes it into `.env` as AI_GATEWAY_API_KEY (and CLOUDFLARE_API_TOKEN),
#     and EMPTIES AI_GATEWAY_URL so the backend uses Workers AI directly.
#  4. Rebuilds/restarts the `vibesdk-backend` container.
#
# Backend behavior (llm/client.go): it appends "/chat/completions" to a base.
# When AI_GATEWAY_URL is empty + CLOUDFLARE_ACCOUNT_ID set, it uses
#   https://api.cloudflare.com/client/v4/accounts/<id>/ai/v1
# which is Workers AI's OpenAI-compatible endpoint (no gateway).
#
# Usage:
#   bash scripts/cf-token-login.sh <your-api-token>
#   CLOUDFLARE_API_TOKEN=<token> bash scripts/cf-token-login.sh
# =============================================================================
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"

ACCOUNT_ID="$(grep -E '^CLOUDFLARE_ACCOUNT_ID=' .env | head -n1 | cut -d= -f2 | tr -d '"')"
if [[ -z "$ACCOUNT_ID" ]]; then
  echo "ERROR: CLOUDFLARE_ACCOUNT_ID must be set in .env ."
  exit 1
fi

TOKEN="${1:-${CLOUDFLARE_API_TOKEN:-}}"
if [[ -z "$TOKEN" ]]; then
  echo "===================================================================="
  echo " Workers AI needs a Cloudflare API Token (one-time setup)."
  echo ""
  echo " The previous 'one-command' flow (wrangler login) only gives an OAuth"
  echo " token. Cloudflare REFUSES OAuth for the Workers AI REST API (401)."
  echo ""
  echo " 1) Open this in your browser and log in:"
  echo "      https://dash.cloudflare.com/profile/api-tokens"
  echo " 2) Create Token -> template 'Workers AI: Read and Write'"
  echo "    (Account Resources: Include -> your account only)"
  echo " 3) Copy the token, then run this same script again:"
  echo "      bash scripts/cf-token-login.sh <the-new-token>"
  echo "===================================================================="
  exit 1
fi

MODEL="${DEFAULT_MODEL:-@cf/qwen/qwen2.5-coder-32b-instruct}"
WORKERS_AI_URL="https://api.cloudflare.com/client/v4/accounts/${ACCOUNT_ID}/ai/v1"

echo "============================================================================"
echo " 1/4 Validating token against account ${ACCOUNT_ID}"
echo "============================================================================"
AUTH_CODE="$(curl -s -m 20 -o /dev/null -w '%{http_code}' \
  -H "Authorization: Bearer $TOKEN" \
  "https://api.cloudflare.com/client/v4/accounts/${ACCOUNT_ID}/ai/models/search?per_page=1" || true)"
echo "      Workers AI models/search -> HTTP $AUTH_CODE"
if [[ "$AUTH_CODE" != "200" ]]; then
  echo "ERROR: token cannot reach Workers AI (HTTP $AUTH_CODE)."
  echo "       Create a token with 'Workers AI → Edit' and try again."
  exit 1
fi

echo
echo "============================================================================"
echo " 2/4 Validating a real chat completion (model: $MODEL)"
echo "============================================================================"
GW_CODE="$(curl -s -m 60 -o /tmp/wa_verify.json -w '%{http_code}' \
  -X POST "${WORKERS_AI_URL}/chat/completions" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" \
  -d "{\"model\":\"${MODEL}\",\"messages\":[{\"role\":\"user\",\"content\":\"Reply with exactly: ok\"}],\"max_tokens\":10,\"stream\":false}" || true)"
echo "      Workers AI chat/completions -> HTTP $GW_CODE"
if [[ "$GW_CODE" != "200" ]]; then
  echo "ERROR: Workers AI returned HTTP $GW_CODE for model $MODEL."
  echo "       The token/site may lack ai:write or the model is unavailable."
  exit 1
fi
echo "      Sample reply: $(head -c 200 /tmp/wa_verify.json)"

echo
echo "============================================================================"
echo " 3/4 Writing token into .env and clearing AI_GATEWAY_URL (Workers AI mode)"
echo "============================================================================"
ENV_FILE="$ROOT/.env"
if [[ -f "$ENV_FILE" ]]; then
  cp "$ENV_FILE" "${ENV_FILE}.cfbak"
  for KEY in AI_GATEWAY_API_KEY CLOUDFLARE_API_TOKEN; do
    if grep -q "^${KEY}=" "$ENV_FILE"; then
      sed -i.cfbak "s|^${KEY}=.*|${KEY}=${TOKEN}|" "$ENV_FILE"
    else
      printf '\n%s=%s\n' "$KEY" "$TOKEN" >> "$ENV_FILE"
    fi
  done
  if grep -q '^AI_GATEWAY_URL=' "$ENV_FILE"; then
    sed -i.cfbak "s|^AI_GATEWAY_URL=.*|AI_GATEWAY_URL=|" "$ENV_FILE"
  else
    printf '\nAI_GATEWAY_URL=\n' >> "$ENV_FILE"
  fi
  rm -f "${ENV_FILE}.cfbak"
  echo "      .env updated (backup kept at ${ENV_FILE}.cfbak)."
else
  echo "ERROR: $ENV_FILE not found."; exit 1
fi

echo
echo "============================================================================"
echo " 4/4 Rebuilding & restarting backend container"
echo "============================================================================"
if command -v docker >/dev/null 2>&1; then
  docker compose up -d --build backend
  echo "      Backend restarted. Check:  docker logs vibesdk-backend"
else
  echo "      'docker' not found - start your backend yourself with the new .env."
fi

echo
echo "✅ Done. Backend now calls Workers AI directly (no AI Gateway)."
echo "   Verify:  docker logs vibesdk-backend | grep -i 'unexpected status 401'"
