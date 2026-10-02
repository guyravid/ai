//go:build mcp

package serve

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/guyravid/ai/cli/tools/trello/internal/app"
	"github.com/guyravid/ai/cli/tools/trello/internal/config"
	"github.com/guyravid/ai/cli/tools/trello/internal/envelope"
	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
	"github.com/guyravid/ai/cli/tools/trello/internal/secrets"
)

const (
	defaultAddr     = "127.0.0.1:7810"
	maxBodyBytes    = 1 << 20
	maxSessions     = 64
	sessionIdleTime = 30 * time.Minute
	shutdownGrace   = 5 * time.Second
)

const (
	tlsModeHTTPS      = "https"
	tlsModeLoopback   = "plaintext on loopback"
	tlsModeTerminated = "plaintext; TLS terminated upstream (operator-declared)"
)

// httpSettings is the validated listener configuration (contract §16.3).
type httpSettings struct {
	addr        string
	remote      bool
	token       string
	certificate *tls.Certificate
	tlsMode     string
}

// resolveHTTP applies every bind, token, and TLS rule before anything listens.
func resolveHTTP(application *app.App, invocation *app.Invocation) (*httpSettings, *errs.Error) {
	prefix, env, goos := application.Build.Prefix, application.Env, application.Build.GOOS
	settings := &httpSettings{}

	addr, remote, err := normalizeAddr(invocation.Flags["addr"])
	if err != nil {
		return nil, err
	}
	settings.addr, settings.remote = addr, remote

	token, err := resolveToken(prefix, env, goos, invocation.Flags["token-file"])
	if err != nil {
		return nil, err
	}
	settings.token = token

	certFile := firstSet(invocation.Flags["tls-cert-file"], lookup(env, prefix+"_MCP_TLS_CERT_FILE"))
	keyFile := firstSet(invocation.Flags["tls-key-file"], lookup(env, prefix+"_MCP_TLS_KEY_FILE"))
	terminated := invocation.Bool("tls-terminated-upstream")
	if value := lookup(env, prefix+"_MCP_TLS_TERMINATED_UPSTREAM"); value != "" && !invocation.Has("tls-terminated-upstream") {
		switch value {
		case "true", "1":
			terminated = true
		case "false", "0":
		default:
			return nil, errs.Configf("%s_MCP_TLS_TERMINATED_UPSTREAM must be true or false.", prefix)
		}
	}
	if (certFile == "") != (keyFile == "") {
		return nil, errs.Usagef("--tls-cert-file and --tls-key-file must be given together.")
	}
	if certFile != "" && terminated {
		return nil, errs.Usagef("Give either a TLS certificate or --tls-terminated-upstream, not both.")
	}

	if remote {
		if !invocation.Bool("allow-remote") {
			return nil, errs.Configf("%s is not a loopback address; serving it needs --allow-remote.", addr).
				WithHint("Bind to 127.0.0.1, or add --allow-remote with a token and a TLS option.")
		}
		if token == "" {
			return nil, errs.Configf("A non-loopback listener needs a token.").
				WithHint("Set " + prefix + "_MCP_TOKEN_FILE=<path> or pass --token-file <path>.")
		}
		if certFile == "" && !terminated {
			return nil, errs.Configf("A non-loopback listener needs TLS: a certificate and key, or --tls-terminated-upstream.").
				WithHint("Add --tls-cert-file <path> --tls-key-file <path>, or --tls-terminated-upstream behind a TLS proxy.")
		}
	}

	switch {
	case certFile != "":
		certificate, err := loadCertificate(certFile, keyFile, env, goos, application.Now())
		if err != nil {
			return nil, err
		}
		settings.certificate, settings.tlsMode = certificate, tlsModeHTTPS
	case terminated:
		settings.tlsMode = tlsModeTerminated
	default:
		settings.tlsMode = tlsModeLoopback
	}
	return settings, nil
}

// normalizeAddr turns a bare port into a loopback address and reports whether the host is remote.
// 0.0.0.0, ::, and any hostname other than localhost count as remote.
func normalizeAddr(addr string) (string, bool, *errs.Error) {
	switch {
	case addr == "":
		addr = defaultAddr
	case !strings.Contains(addr, ":"):
		addr = "127.0.0.1:" + addr
	case strings.HasPrefix(addr, ":"):
		addr = "127.0.0.1" + addr
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return "", false, errs.Usagef("--addr must be host:port or a port, such as 127.0.0.1:7810.")
	}
	return addr, !isLoopbackHost(host), nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// resolveToken follows §16.3's order: <TOOL>_MCP_TOKEN, <TOOL>_MCP_TOKEN_FILE, then --token-file.
// Token files are checked like credential files (§12.4).
func resolveToken(prefix string, env config.Env, goos, flagPath string) (string, *errs.Error) {
	if value := lookup(env, prefix+"_MCP_TOKEN"); value != "" {
		return value, nil
	}
	source, path := prefix+"_MCP_TOKEN_FILE", lookup(env, prefix+"_MCP_TOKEN_FILE")
	if path == "" {
		source, path = "--token-file", flagPath
	}
	if path == "" {
		return "", nil
	}
	expanded, err := config.ExpandHome(path, env, goos)
	if err != nil {
		return "", errs.Configf("Cannot expand %s: %s.", source, err.Error())
	}
	value, _, readErr := secrets.ReadProtected(expanded, goos)
	if readErr != nil {
		return "", readErr.WithDetail("source", source)
	}
	if strings.TrimSpace(value) == "" {
		return "", errs.Configf("The token file named by %s is empty.", source)
	}
	return strings.TrimSpace(value), nil
}

// loadCertificate validates the pair at startup, so a bad file fails now and not on the first
// handshake: the key is permission-checked, the pair must load and match, and the leaf must be
// within its validity period.
func loadCertificate(certFile, keyFile string, env config.Env, goos string, now time.Time) (*tls.Certificate, *errs.Error) {
	certPath, err := config.ExpandHome(certFile, env, goos)
	if err != nil {
		return nil, errs.Configf("Cannot expand the TLS certificate path: %s.", err.Error())
	}
	keyPath, err := config.ExpandHome(keyFile, env, goos)
	if err != nil {
		return nil, errs.Configf("Cannot expand the TLS key path: %s.", err.Error())
	}
	if _, checkErr := secrets.CheckProtected(keyPath, goos); checkErr != nil {
		return nil, checkErr.WithDetail("source", "tls-key-file")
	}
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, errs.Configf("The TLS certificate and key cannot be loaded: %s.", err.Error())
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, errs.Configf("The TLS certificate cannot be parsed: %s.", err.Error())
	}
	if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		return nil, errs.Configf("The TLS certificate is not valid now (valid %s to %s).",
			leaf.NotBefore.UTC().Format(time.RFC3339), leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	return &pair, nil
}

func runHTTP(ctx context.Context, application *app.App, serverFlags map[string]string, settings *httpSettings) *app.Response {
	server := newServer(ctx, application, app.TransportHTTP, serverFlags)
	handler := harden(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{SessionTimeout: sessionIdleTime, MaxRequestBodyBytes: maxBodyBytes}),
		hardening{token: settings.token, health: healthDocument(application), sessions: func() int { return countSessions(server) }})

	listener, err := net.Listen("tcp", settings.addr)
	if err != nil {
		return application.Fail("serve", errs.Configf("Cannot listen on %s: %s.", settings.addr, err.Error()))
	}
	httpServer := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
		// Requests inherit the server's context, so a shutdown also ends open event streams and any
		// tool call still running, instead of Shutdown waiting for them.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	if settings.certificate != nil {
		httpServer.TLSConfig.Certificates = []tls.Certificate{*settings.certificate}
		listener = tls.NewListener(listener, httpServer.TLSConfig)
	}

	auth := "off"
	if settings.token != "" {
		auth = "on"
	}
	logLine(application.Stderr, "info", "serve started", map[string]any{
		"transport": "http", "addr": listener.Addr().String(), "writes_enabled": application.Build.WritesEnabled,
		"auth": auth, "tls": settings.tlsMode,
	})
	if settings.remote && application.Build.WritesEnabled {
		logLine(application.Stderr, "warn", "writes are enabled on a non-loopback listener; any authenticated client can change Trello. Prefer the read-only build (trello-ro).", nil)
	}

	served := make(chan error, 1)
	go func() { served <- httpServer.Serve(listener) }()
	select {
	case err = <-served:
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		err = httpServer.Shutdown(shutdownCtx)
		if errors.Is(err, context.DeadlineExceeded) {
			err = httpServer.Close()
		}
	}
	logLine(application.Stderr, "info", "serve stopped", map[string]any{"transport": "http"})
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		logLine(application.Stderr, "error", "serve failed", map[string]any{"error": err.Error()})
		return &app.Response{Quiet: true, Exit: 1}
	}
	return &app.Response{Quiet: true}
}

func countSessions(server *mcp.Server) int {
	count := 0
	for range server.Sessions() {
		count++
	}
	return count
}

// hardening is what harden enforces besides the SDK's own body cap and session expiry.
type hardening struct {
	token    string
	health   []byte
	sessions func() int
}

// harden wraps the MCP handler, in order: unauthenticated /health, Origin check, body cap, bearer
// token in constant time, and a bound on new sessions (base template §16.2).
func harden(next http.Handler, rules hardening) http.Handler {
	want := []byte("Bearer " + rules.token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.Write(rules.health)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !isLoopbackOrigin(origin) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		if rules.token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodPost && r.Header.Get("Mcp-Session-Id") == "" && rules.sessions() >= maxSessions {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Hostname() != "" && isLoopbackHost(parsed.Hostname())
}

// healthDocument holds only what §16.3 allows: no addresses, paths, credentials, or commands.
func healthDocument(application *app.App) []byte {
	encoded, _ := json.Marshal(map[string]any{
		"tool": application.Build.Tool, "tool_version": application.Build.Version,
		"contract_version": envelope.ContractVersion,
		"writes_enabled":   application.Build.WritesEnabled, "mcp_enabled": application.Build.MCPEnabled,
	})
	return encoded
}

func lookup(env config.Env, name string) string {
	value, _ := env(name)
	return value
}

func firstSet(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
