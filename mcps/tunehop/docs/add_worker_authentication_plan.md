# Tunehop Worker Authentication Plan

We will secure your `tunehop` Cloudflare Worker by implementing **Bearer Token Authentication**. This ensures that even if someone discovers your `workers.dev` URL, they cannot use your MCP or consume your Odesli API rate limits without the secret token.

## Proposed Architecture

We will introduce an optional environment variable `AUTH_TOKEN`. 
If `AUTH_TOKEN` is configured on the worker, the worker will intercept every incoming HTTP request and require an `Authorization: Bearer <TOKEN>` header.

### 1. Update `tunehop/src/index.ts`
- **Env Interface:** Add `AUTH_TOKEN?: string;` to the `Env` interface.
- **Fetch Handler:** Add an intercept block at the very beginning of the `fetch` function:
  ```typescript
  if (request.method !== "OPTIONS" && env.AUTH_TOKEN) {
    const authHeader = request.headers.get("Authorization");
    if (authHeader !== `Bearer ${env.AUTH_TOKEN}`) {
      return new Response("Unauthorized", { status: 401 });
    }
  }
  ```
  *(Note: We skip `OPTIONS` requests to ensure CORS preflight checks don't fail if you ever connect a web-based MCP client).*

### 2. Update `tunehop/README.md`
- Add a new **Security / Authentication** section explaining how to generate a token and apply it via `npx wrangler secret put AUTH_TOKEN` or locally in `.dev.vars`.
- Update the **Connect to Claude Code** examples to show the `-H "Authorization: Bearer <token>"` flag.

## User Review Required
> [!IMPORTANT]
> - By making `AUTH_TOKEN` optional, your current deployment won't break immediately until you explicitly add the secret to your Cloudflare Worker. Do you prefer it to be strictly required instead?
> - Does this plan align with how you intend to secure the worker?

If this plan looks good, please approve and I will execute the changes!
