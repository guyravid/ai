//go:build !mcp

package main

import "github.com/guyravid/ai/cli/tools/telegram/internal/app"

// serveFunc is nil without the mcp build tag: serve is refused with mcp_disabled, and no server
// code is linked in.
var serveFunc app.ServeFunc
