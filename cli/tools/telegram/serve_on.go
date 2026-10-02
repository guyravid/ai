//go:build mcp

package main

import "github.com/guyravid/ai/cli/tools/telegram/internal/serve"

// serveFunc runs the MCP server; only builds with the mcp tag link it.
var serveFunc = serve.Run
