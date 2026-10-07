# Parallel Search MCP

Search the web or fetch a page through the SDK's tool registry using the
[Parallel Search MCP](https://docs.parallel.ai/integrations/mcp/search-mcp).
The anonymous endpoint is free for exploration and light use, subject to rate
limits. No Parallel API key or model API key is needed for this example.

From a clean checkout, with Go 1.24 or later:

```bash
go mod download
go run ./examples/14-parallel-search -query "Go context cancellation documentation"
go run ./examples/14-parallel-search -url https://go.dev/blog/context
```

Output includes the session ID and the server's text response containing source
URLs and excerpts. Pass the same `-session-id` on related searches and fetches:

```bash
go run ./examples/14-parallel-search -query "Go context cancellation documentation" -session-id my-research
go run ./examples/14-parallel-search -url https://go.dev/blog/context -session-id my-research
```

The example uses `RegisterMCPServerWithOptions` and `Registry.Execute`, allowing
only `web_search` and `web_fetch`. The `https+http://` spec selects the SDK's
Streamable HTTP transport and resolves to `https://search.parallel.ai/mcp`.
It sends `User-Agent: agentsdk-go/parallel-search-example` on discovery and tool
requests. Connection and tool timeouts are 20 and 60 seconds, with a 90-second
overall deadline and Ctrl-C cancellation.

This example dispatches tools directly. It does not run an agent loop or load
saved settings, credentials, or environment keys. To expose these tools to your
own model, register them alongside your other tools; model inference has its own
configuration and costs. Existing SDK defaults are unchanged.

Offline validation:

```bash
go test ./examples/14-parallel-search
```
