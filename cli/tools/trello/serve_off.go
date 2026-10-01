//go:build !mcp

package main

import "github.com/guyravid/ai/cli/tools/trello/internal/app"

// serveFunc is nil without the mcp build tag: serve is refused with mcp_disabled, and no server
// code is linked in.
var serveFunc app.ServeFunc
