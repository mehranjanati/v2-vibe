# VibeSDK Setup Guide — legacy V1 snapshot (historical)

> **Historical / V1 only — removed in the dual-plane migration.** This file preserves the parts of
> `docs/setup.md` that described the pre-migration single-Worker layout (`wrangler.jsonc`,
> ThinkAgent/SpaceDO previews, R2 templates, dispatch namespaces, sandbox containers) together with
> the interactive `bun run setup` script's prompt flow.
>
> **Do not follow these instructions for the current tree** — `wrangler.jsonc` no longer exists, and
> `scripts/setup.ts` aborts on it (`scripts/setup.ts:1133`). The live setup path is
> [`../setup.md`](../setup.md); the architecture is described in [`../llm.md`](../llm.md).
>
> Moved by docs-audit task T13 (2026-09-27) — see [`../DOCS_AUDIT_BACKLOG.md`](../DOCS_AUDIT_BACKLOG.md).
> Text below is the V1 wording, kept verbatim for history.

## What You'll Need During Setup (V1 script)

The setup script will ask you for the following information:

### Cloudflare Account Information

1. **Account ID**: Found in your Cloudflare dashboard sidebar
2. **API Token**: In you Cloudflare dashboard under "My Profile" > "API Tokens", create a token (Using the "Edit Cloudflare Workers" template is recommended) with the following configurations:
   - Your Account - Workers KV Storage:Edit, Workers Scripts:Edit, Account Settings:Read, Workers Tail:Read, Workers R2 Storage:Edit, Cloudflare Pages:Edit, Workers Builds Configuration:Edit, Workers Agents Configuration:Edit, Workers Observability:Edit, Containers:Edit, D1:Edit, AI Gateway:Read, AI Gateway:Edit, AI Gateway:Run, Cloudchamber:Edit, Browser Rendering:Edit
   - All zones - Workers Routes:Edit
   - All users - User Details:Read, Memberships:Read

   **If using the `Edit Cloudflare Workers` template, make sure to add the missing permissions above manually.**

   **Important**: Some features like D1 databases and R2 may require a paid Cloudflare plan.

### Domain Configuration

**With Custom Domain:**
```bash
Enter your custom domain (or press Enter to skip): myapp.com
✅ Custom domain set: myapp.com
Use remote Cloudflare resources (KV, D1, R2, etc.)? (Y/n): 
Configure for production deployment? (Y/n): 
```

**Without Custom Domain:**
```bash
Enter your custom domain (or press Enter to skip): [press Enter]
⚠️  No custom domain provided.
   • Remote Cloudflare resources: Not available
   • Production deployment: Not available
   • Only local development will be configured

Continue with local-only setup? (Y/n): 
```

### AI Gateway Configuration (V1)

**Cloudflare AI Gateway (Recommended)**
- **Automatic token setup**: When selected, `CLOUDFLARE_AI_GATEWAY_TOKEN` is automatically set to your API token
- **No manual configuration**: The script handles all AI Gateway authentication
- **Better performance**: Caching, rate limiting, and monitoring included

**Custom OpenAI URL (Alternative)**
- For users with existing OpenAI-compatible endpoints
- Requires manual model configuration in `worker/agents/inferutils/config.ts`

### AI Provider Selection (V1)

The setup script offers multiple AI providers with intelligent multi-selection:

**Available Providers:**
1. **OpenAI** (for GPT models)
2. **Anthropic** (for Claude models)  
3. **Google AI Studio** (for Gemini models) - **Default & Recommended**
4. **Cerebras** (for open source models)
5. **OpenRouter** (for various models)
6. **Custom provider** (for any other provider)

**Provider Selection:**
- Select multiple providers with comma-separated numbers (e.g., `1,2,3`)
- Each selected provider will prompt for its API key
- Custom providers automatically generate `PROVIDER_NAME_API_KEY` variables
- Custom providers are automatically added to `worker-configuration.d.ts`

### Important Model Configuration Notes (V1)

**Google AI Studio (Recommended):**
- Default model configurations use Gemini models
- No additional `worker/agents/inferutils/config.ts` editing required
- Best compatibility - This is the model used in the official deployment at https://build.cloudflare.dev
- You can get a free API key from https://aistudio.google.com/

**Other Providers:**
- **Strong warning**: You MUST edit `worker/agents/inferutils/config.ts` 
- Change default model configurations from Gemini to your selected providers
- Model format: `<provider-name>/<model-name>` (e.g., `openai/gpt-4`, `anthropic/claude-3.5-sonnet`)
- Review fallback model configurations

**Without AI Gateway:**
- **Manual config.ts editing required** for all model configurations
- Model names must follow `<provider-name>/<model-name>` format

### OAuth Configuration (V1)

The script will also ask for OAuth credentials:

- **Google OAuth**: For user authentication and login (not AI Studio access)
- **GitHub OAuth**: For user authentication and login
- **GitHub Export OAuth**: For exporting generated apps to GitHub repositories (separate from login OAuth)

**If you don't provide OAuth credentials, by default at login, you will only be able to use email-based registration/login.**

### Login with Cloudflare (V1)

You can let users sign in with their Cloudflare account. The same consent also
connects their Cloudflare AI Gateway, so generations can run on their own credits
("Use my AI Gateway" toggle in settings).

**1. Create an OAuth client**

Create an OAuth client in the Cloudflare dashboard:
<https://dash.cloudflare.com/?to=/:account/oauth-clients>

Configure these **redirect URLs** on the client (replace the origin with your
deployment's URL; for local development this is `http://localhost:5173`):

- `https://your-domain.com/api/auth/callback/cloudflare` — "Login with Cloudflare"
- `https://your-domain.com/auth/callback` — connect AI Gateway (from settings)

Grant the client these **scopes** (Cloudflare uses dotted identifiers, not OIDC
`email`/`profile`):

```
openid user-details.read ai.read ai.write aig.read aig.run aig.write offline_access
```

The scopes and the Cloudflare OAuth endpoint URLs are hardcoded in the worker
(`worker/services/oauth/cloudflare-connect.ts`) and are not configurable — just make
sure the OAuth client is authorized for all of these scopes, or the authorization
request fails with `invalid_scope`.

**2. Set the environment variables**

Add the client credentials to `.dev.vars` (and `.prod.vars` for production):

```bash
CLOUDFLARE_OAUTH_CLIENT_ID="<your-oauth-client-id>"        # required for Login with Cloudflare
CLOUDFLARE_OAUTH_CLIENT_SECRET="<your-oauth-client-secret>"
CF_OAUTH_ENCRYPTION_KEY="<32-byte base64 key>"             # required for AI Gateway; encrypts the token cookie
```

Set `ENABLE_CLOUDFLARE_LIMITS="true"` in the Cloudflare dashboard for production, or in `.dev.vars` for local development.

The **"Login with Cloudflare" button** appears as soon as `CLOUDFLARE_OAUTH_CLIENT_ID`
and `CLOUDFLARE_OAUTH_CLIENT_SECRET` are set — identity login needs nothing else.

The **AI Gateway connect/auto-connect** (running generations on the user's own
credits) additionally requires the dashboard-managed `ENABLE_CLOUDFLARE_LIMITS="true"` and
`CF_OAUTH_ENCRYPTION_KEY` (generate with `openssl rand -base64 32`). If the key is
missing, the gateway feature is disabled (same as leaving `ENABLE_CLOUDFLARE_LIMITS`
unset) and login simply skips the gateway auto-connect — users fall back to the free
tier and can connect later.

> **Current-tree reality (T12/T13):** the light Worker's `/api/auth/providers` reports
> `cloudflare: false` and `google: false`, and neither plane reads `CLOUDFLARE_OAUTH_*`,
> `CF_OAUTH_ENCRYPTION_KEY` or `ENABLE_CLOUDFLARE_LIMITS`, so the flow above is not wired
> in the dual-plane tree yet (`worker/light/lightApp.ts:368`).

## V1 production and file-layout notes

### Production-Only Setup (V1)

If you only set up for local development initially, you can configure production later:

1. **Run setup again** and choose "yes" for remote deployment configuration
2. **Provide production domain** when prompted
3. **Deploy** using `bun run deploy`

### Manual Production Setup (V1)

Alternatively, create `.prod.vars` manually based on `.dev.vars` but with:
- Production domain in `CUSTOM_DOMAIN`
- Production API keys and secrets
- `ENVIRONMENT="prod"`

### File Structure After Setup (V1)

The setup script creates and modifies these files:

```
vibesdk/
├── .dev.vars              # Local development environment variables
├── .prod.vars             # Production environment variables (if configured)
├── wrangler.jsonc         # Updated with resource IDs and domain
├── vite.config.ts         # Updated for remote/local bindings
├── migrations/            # Database migration files
└── templates/             # Template repository (downloaded)
```

### Summary of the V1 script

The VibeSDK setup script provides a comprehensive, intelligent configuration experience:

**Key Features:** simplified domain setup; intelligent AI provider selection; AI Gateway automation; custom provider support; production-ready local + production configuration; user-friendly defaults.

**What it configured:** Cloudflare resources (KV, D1, R2, AI Gateway, dispatch namespaces); environment variables (`.dev.vars` and `.prod.vars`); Worker configuration (`wrangler.jsonc`, `worker-configuration.d.ts`); database setup and migrations; template deployment; ARM64 compatibility.

### V1-only troubleshooting entries

**R2 Bucket "Unauthorized" Error**: This usually means:
- Your API token lacks "R2:Edit" permissions
- Your account doesn't have access to R2 (may require paid plan)
- You've exceeded your R2 bucket quota
- **Solution**: Update your API token permissions or upgrade your Cloudflare plan

**AI Configuration Issues (V1)**:
- **"AI Gateway token already configured" but token not in .dev.vars**: Re-run setup, this was a bug that's now fixed
- **Models not working with custom providers**: Edit `worker/agents/inferutils/config.ts` to change default model configurations
- **Custom provider not recognized**: Check that the provider was added to `worker-configuration.d.ts`
- **AI Gateway creation failed**: Ensure your API token has AI Gateway permissions

**Resource Creation Failed (V1)**: Check that your account has available KV namespace quota (10 on free plan), D1 database quota, R2 bucket quota and an appropriate plan level for the requested features.

## Corporate CA certificate block (V1 sandbox Dockerfile)

This block lived in the troubleshooting section of `docs/setup.md` and referenced the deleted
`SandboxDockerfile`. It is kept here as history only.

The `SandboxDockerfile` these steps edit was removed with the dual-plane migration, so the certificate setup below is kept only as historical reference:

1. **Copy your corporate root CA certificate** to the project root (don't commit to git!)
2. **Edit SandboxDockerfile** to include your certificate:

```dockerfile
# Add your company's Root CA certificate for corporate network access
COPY your-root-ca.pem /usr/local/share/ca-certificates/your-root-ca.crt
RUN update-ca-certificates

# Set SSL environment variables for cloudflared and other tools
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
ENV NODE_EXTRA_CA_CERTS=/usr/local/share/ca-certificates/your-root-ca.crt
ENV CURL_CA_BUNDLE=/etc/ssl/certs/ca-certificates.crt
```

**⚠️ Security Warning**: Never commit corporate CA certificates to public repositories. Use `.gitignore` to exclude certificate files and only use this for local development.
