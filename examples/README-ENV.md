# Environment Quickstart for Examples

- Copy the template: `cp .env.example .env` (the resulting `.env` stays ignored, no secrets in git).
- Fill **required** variables in `.env`:
  - `ANTHROPIC_API_KEY` - Your Anthropic API key (required for all examples)
  - OR `ANTHROPIC_AUTH_TOKEN` - (deprecated) Legacy auth token, use ANTHROPIC_API_KEY instead
- **Optional** variables you can adjust:
  - `ANTHROPIC_BASE_URL` - Custom API endpoint (for proxies or private deployments)
  - `AGENTSDK_MODEL` - Override default model (default: `claude-sonnet-4-5-20250929`)
  - `AGENTSDK_HTTP_ADDR` - HTTP server address for example 03 (default: `:8080`)
- Load the values when running examples:
  - One-time per shell: `source .env`
  - Inline per command (no shell state): `env $(cat .env | xargs) go run .`
- What each example reads:
  - `examples/01-basic`: `ANTHROPIC_API_KEY` (or `ANTHROPIC_AUTH_TOKEN`), optional `AGENTSDK_MODEL`, optional `ANTHROPIC_BASE_URL`.
  - `examples/02-cli`: same as 01-basic.
  - `examples/03-http`: `ANTHROPIC_API_KEY` (or `ANTHROPIC_AUTH_TOKEN`), `AGENTSDK_HTTP_ADDR` (default :8080), optional `AGENTSDK_MODEL`, optional `ANTHROPIC_BASE_URL`.
  - `examples/04-advanced`: same as 01-basic.
