//go:build mcp

// Package serve runs the tool as an MCP server over stdio or Streamable HTTP (contract §16). Every
// tool call goes through app.ExecuteTool, the same response path as the command line.
package serve

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/guyravid/ai/cli/tools/trello/internal/app"
	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
)

// inherited are the serve flags every tool call inherits.
var inherited = []string{"profile", "config", "deterministic", "verbose"}

// httpOnly are the serve flags that mean nothing over stdio.
var httpOnly = []string{"addr", "allow-remote", "token-file", "tls-cert-file", "tls-key-file", "tls-terminated-upstream"}

// Run is the app.ServeFunc. It returns a quiet response once the server stops, so nothing reaches
// stdout after the protocol has used it; a failure before starting is a normal envelope.
func Run(ctx context.Context, application *app.App, invocation *app.Invocation) *app.Response {
	serverFlags := map[string]string{}
	for _, name := range inherited {
		if value, ok := invocation.Flags[name]; ok {
			serverFlags[name] = value
		}
	}
	transport := invocation.Flags["transport"]
	switch transport {
	case "", string(app.TransportStdio):
		for _, name := range httpOnly {
			if invocation.Has(name) {
				return application.Fail("serve", errs.Usagef("--%s applies only to --transport http.", name).
					WithDetail("flag", "--"+name))
			}
		}
		return runStdio(ctx, application, serverFlags)
	case string(app.TransportHTTP):
		settings, err := resolveHTTP(application, invocation)
		if err != nil {
			return application.Fail("serve", err)
		}
		return runHTTP(ctx, application, serverFlags, settings)
	}
	return application.Fail("serve", errs.Usagef("--transport must be stdio or http.").
		WithDetail("allowed", []string{"stdio", "http"}))
}

// NewServer builds an MCP server publishing the commands offered over a transport.
func NewServer(application *app.App, transport app.Transport, serverFlags map[string]string) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: application.Build.Tool, Version: application.Build.Version},
		&mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}}},
	)
	for _, command := range application.ToolCommands(transport) {
		annotations := command.Annotations
		openWorld := annotations.OpenWorldHint
		tool := &mcp.Tool{
			Name:        command.Name,
			Description: command.Description,
			InputSchema: app.ToolInputSchema(command),
			Annotations: &mcp.ToolAnnotations{
				ReadOnlyHint:    annotations.ReadOnlyHint,
				IdempotentHint:  annotations.IdempotentHint,
				DestructiveHint: annotations.DestructiveHint,
				OpenWorldHint:   &openWorld,
			},
		}
		// A typed nil would be published as a null schema, so only set one that exists.
		if output := app.ToolOutputSchema(command); output != nil {
			tool.OutputSchema = output
		}
		server.AddTool(tool, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			response := application.ExecuteTool(ctx, app.ToolCall{
				Command: command, Arguments: request.Params.Arguments,
				ServerFlags: serverFlags, Transport: transport,
			})
			defer response.Cleanup()
			return toResult(response), nil
		})
	}
	return server
}

// toResult returns the envelope as structured content and as one text block, with isError = !ok
// (contract §16.2). teach's Markdown is text only.
func toResult(response *app.Response) *mcp.CallToolResult {
	document := response.Document()
	result := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(document)}}}
	if response.Text == "" {
		result.StructuredContent = json.RawMessage(document)
		result.IsError = response.Envelope == nil || !response.Envelope.OK
	}
	return result
}

func runStdio(ctx context.Context, application *app.App, serverFlags map[string]string) *app.Response {
	server := NewServer(application, app.TransportStdio, serverFlags)
	logLine(application.Stderr, "info", "serve started", map[string]any{
		"transport": "stdio", "writes_enabled": application.Build.WritesEnabled, "auth": "off",
	})
	err := server.Run(ctx, &mcp.StdioTransport{})
	logLine(application.Stderr, "info", "serve stopped", map[string]any{"transport": "stdio"})
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
		logLine(application.Stderr, "error", "serve failed", map[string]any{"error": err.Error()})
		return &app.Response{Quiet: true, Exit: 1}
	}
	return &app.Response{Quiet: true}
}

// logLine writes one JSON line to stderr, in the same shape as the tool's logs. It never carries a
// secret: callers pass only transport facts.
func logLine(stderr io.Writer, level, message string, fields map[string]any) {
	line := map[string]any{"level": level, "msg": message}
	for key, value := range fields {
		line[key] = value
	}
	encoded, _ := json.Marshal(line)
	stderr.Write(append(encoded, '\n'))
}
