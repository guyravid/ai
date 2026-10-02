//go:build mcp

package serve

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/guyravid/ai/cli/tools/trello/internal/app"
	"github.com/guyravid/ai/cli/tools/trello/internal/commands"
	"github.com/guyravid/ai/cli/tools/trello/internal/registry"
)

var fixedNow = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

// syncBuffer is a stderr that tests read while the server writes.
type syncBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func newApp(t *testing.T, env map[string]string) (*app.App, *syncBuffer) {
	t.Helper()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/1/members/me/boards" {
			fmt.Fprint(w, `[{"id":"b1","name":"Alpha","closed":false}]`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(fake.Close)
	home := t.TempDir()
	base := map[string]string{
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, "config"), "XDG_CACHE_HOME": t.TempDir(),
		"TRELLO_BASE_URL": fake.URL + "/1", "TRELLO_API_KEY": "k", "TRELLO_API_TOKEN": "t",
	}
	for key, value := range env {
		base[key] = value
	}
	all := append(registry.Builtins(), commands.Reads()...)
	all = append(all, commands.Writes()...)
	stderr := &syncBuffer{}
	return &app.App{
		Build: app.Build{Tool: "trello", Prefix: "TRELLO", Version: "test", WritesEnabled: commands.WritesEnabled,
			MCPEnabled: true, GOOS: "linux", GOARCH: "arm64"},
		Registry:      registry.New(all),
		Env:           func(name string) (string, bool) { value, ok := base[name]; return value, ok },
		Now:           func() time.Time { return fixedNow },
		Stderr:        stderr,
		MutatingNames: commands.MutatingNames,
	}, stderr
}

func connect(t *testing.T, server *mcp.Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { serverSession.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func canonical(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var generic any
	if err := json.Unmarshal(encoded, &generic); err != nil {
		t.Fatal(err)
	}
	encoded, _ = json.Marshal(generic)
	return string(encoded)
}

// describeEntries runs describe through the command line, the source of truth for parity.
func describeEntries(t *testing.T, application *app.App) map[string]map[string]any {
	t.Helper()
	var stdout bytes.Buffer
	application.Run(context.Background(), []string{"describe"}, &stdout, func() int { return 0 })
	var document struct {
		Data struct {
			Commands []map[string]any `json:"commands"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	entries := map[string]map[string]any{}
	for _, entry := range document.Data.Commands {
		entries[entry["name"].(string)] = entry
	}
	return entries
}

// TestToolListMatchesDescribe is the parity proof: every published tool has describe's name,
// description, annotations, and parameters, and its output schema wraps describe's outputSchema.
func TestToolListMatchesDescribe(t *testing.T) {
	application, _ := newApp(t, nil)
	described := describeEntries(t, application)
	for _, transport := range []app.Transport{app.TransportStdio, app.TransportHTTP} {
		session := connect(t, NewServer(application, transport, nil))
		listed, err := session.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		var want []string
		for _, command := range application.ToolCommands(transport) {
			want = append(want, command.Name)
		}
		var got []string
		for _, tool := range listed.Tools {
			got = append(got, tool.Name)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s tools = %v, want %v", transport, got, want)
		}
		for _, tool := range listed.Tools {
			entry := described[tool.Name]
			if entry == nil {
				t.Fatalf("%s is not in describe", tool.Name)
			}
			if tool.Description != entry["description"] {
				t.Errorf("%s description differs", tool.Name)
			}
			if canonical(t, tool.Annotations) != canonical(t, entry["annotations"]) {
				t.Errorf("%s annotations %s, describe %s", tool.Name, canonical(t, tool.Annotations), canonical(t, entry["annotations"]))
			}
			input := tool.InputSchema.(map[string]any)
			describedInput := entry["inputSchema"].(map[string]any)
			describedProperties, _ := describedInput["properties"].(map[string]any)
			properties := input["properties"].(map[string]any)
			for name, property := range describedProperties {
				if canonical(t, properties[name]) != canonical(t, property) {
					t.Errorf("%s parameter %s differs from describe", tool.Name, name)
				}
			}
			if canonical(t, input["required"]) != canonical(t, describedInput["required"]) {
				t.Errorf("%s required differs from describe", tool.Name)
			}
			for name := range properties {
				if _, ok := describedProperties[name]; ok {
					continue
				}
				flag := registry.ReservedFlag(strings.ReplaceAll(name, "_", "-"))
				if flag == nil || !flag.MCPExtra {
					t.Errorf("%s argument %s is neither a parameter nor a per-call flag", tool.Name, name)
				}
			}
			if tool.Name == "teach" {
				if tool.OutputSchema != nil {
					t.Error("teach has no output schema")
				}
				continue
			}
			data := tool.OutputSchema.(map[string]any)["properties"].(map[string]any)["data"].(map[string]any)["anyOf"].([]any)
			if canonical(t, data[0]) != canonical(t, entry["outputSchema"]) || canonical(t, data[1]) != `{"type":"null"}` {
				t.Errorf("%s output data schema differs from describe", tool.Name)
			}
		}
	}
}

func TestReadOnlyBuildPublishesNoWrites(t *testing.T) {
	if commands.WritesEnabled {
		t.Skip("full build")
	}
	application, _ := newApp(t, nil)
	listed, err := connect(t, NewServer(application, app.TransportStdio, nil)).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		for _, name := range commands.MutatingNames {
			if tool.Name == name {
				t.Errorf("read-only build publishes %s", name)
			}
		}
		if !tool.Annotations.ReadOnlyHint && tool.Name != "dataset.rm" && tool.Name != "dataset.clear" {
			t.Errorf("%s is not read-only", tool.Name)
		}
	}
}

func call(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCallReturnsEnvelopeTwice(t *testing.T) {
	application, _ := newApp(t, nil)
	session := connect(t, NewServer(application, app.TransportStdio, nil))

	ok := call(t, session, "boards.list", map[string]any{"limit": 5})
	if ok.IsError || len(ok.Content) != 1 {
		t.Fatalf("boards.list: isError=%v content=%d", ok.IsError, len(ok.Content))
	}
	text := ok.Content[0].(*mcp.TextContent).Text
	if canonical(t, ok.StructuredContent) != canonical(t, json.RawMessage(text)) {
		t.Errorf("structured content and text differ:\n%v\n%s", ok.StructuredContent, text)
	}
	if !strings.Contains(text, `"ok":true`) || !strings.Contains(text, `"Alpha"`) {
		t.Errorf("unexpected envelope %s", text)
	}

	failed := call(t, session, "boards.list", map[string]any{"bogus": true})
	if !failed.IsError || !strings.Contains(failed.Content[0].(*mcp.TextContent).Text, `"code":"usage"`) {
		t.Errorf("an unknown argument must be a usage envelope with isError: %+v", failed)
	}

	teach := call(t, session, "teach", nil)
	if teach.IsError || teach.StructuredContent != nil || !strings.HasPrefix(teach.Content[0].(*mcp.TextContent).Text, "#") {
		t.Errorf("teach should return Markdown text only: %+v", teach)
	}
}

func TestCallWithoutConfigurationStillLists(t *testing.T) {
	application, _ := newApp(t, map[string]string{"TRELLO_API_KEY": "", "TRELLO_API_TOKEN": ""})
	session := connect(t, NewServer(application, app.TransportStdio, nil))
	if _, err := session.ListTools(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	result := call(t, session, "boards.list", nil)
	if !result.IsError || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, `"code":"auth"`) {
		t.Errorf("a call with no credential is an auth envelope: %+v", result.Content[0])
	}
}

func TestHarden(t *testing.T) {
	sessions := 0
	handler := harden(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}), hardening{token: "s3cret", health: []byte(`{"tool":"trello"}`), sessions: func() int { return sessions }})
	server := httptest.NewServer(handler)
	defer server.Close()

	send := func(method, path string, body io.Reader, headers map[string]string) *http.Response {
		t.Helper()
		request, _ := http.NewRequest(method, server.URL+path, body)
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response
	}
	auth := map[string]string{"Authorization": "Bearer s3cret"}
	cases := []struct {
		name    string
		method  string
		path    string
		body    io.Reader
		headers map[string]string
		want    int
	}{
		{"health needs no token", "GET", "/health", nil, nil, 200},
		{"no token", "POST", "/mcp", strings.NewReader("{}"), nil, 401},
		{"wrong token", "POST", "/mcp", strings.NewReader("{}"), map[string]string{"Authorization": "Bearer nope"}, 401},
		{"right token", "POST", "/mcp", strings.NewReader("{}"), auth, 200},
		{"loopback origin", "POST", "/mcp", strings.NewReader("{}"), map[string]string{"Authorization": "Bearer s3cret", "Origin": "http://localhost:3000"}, 200},
		{"remote origin", "POST", "/mcp", strings.NewReader("{}"), map[string]string{"Authorization": "Bearer s3cret", "Origin": "https://evil.example"}, 403},
		{"remote origin before auth", "POST", "/mcp", nil, map[string]string{"Origin": "https://evil.example"}, 403},
		{"body over 1 MiB", "POST", "/mcp", bytes.NewReader(make([]byte, maxBodyBytes+1)), auth, 413},
	}
	for _, tc := range cases {
		if got := send(tc.method, tc.path, tc.body, tc.headers).StatusCode; got != tc.want {
			t.Errorf("%s: status %d, want %d", tc.name, got, tc.want)
		}
	}
	sessions = maxSessions
	if got := send("POST", "/mcp", strings.NewReader("{}"), auth).StatusCode; got != http.StatusServiceUnavailable {
		t.Errorf("new session over the bound: %d", got)
	}
	withSession := map[string]string{"Authorization": "Bearer s3cret", "Mcp-Session-Id": "abc"}
	if got := send("POST", "/mcp", strings.NewReader("{}"), withSession).StatusCode; got != 200 {
		t.Errorf("an existing session must not be bounded: %d", got)
	}
}

func TestHealthDocumentHoldsOnlyAllowedFields(t *testing.T) {
	application, _ := newApp(t, nil)
	var health map[string]any
	if err := json.Unmarshal(healthDocument(application), &health); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"tool": true, "tool_version": true, "contract_version": true, "writes_enabled": true, "mcp_enabled": true}
	for key := range health {
		if !allowed[key] {
			t.Errorf("health exposes %s", key)
		}
	}
	if len(health) != len(allowed) {
		t.Errorf("health = %v", health)
	}
}

func writeFile(t *testing.T, path string, content []byte, mode os.FileMode) string {
	t.Helper()
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// selfSigned writes a certificate valid between notBefore and notAfter, and its key.
func selfSigned(t *testing.T, dir string, notBefore, notAfter time.Time, keyMode os.FileMode) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: notBefore, NotAfter: notAfter, DNSNames: []string{"localhost"}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath := writeFile(t, filepath.Join(dir, "cert.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
	keyPath := writeFile(t, filepath.Join(dir, "key.pem"), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), keyMode)
	return certPath, keyPath
}

func TestResolveHTTP(t *testing.T) {
	dir := t.TempDir()
	cert, key := selfSigned(t, dir, fixedNow.Add(-time.Hour), fixedNow.Add(time.Hour), 0o600)
	expiredDir := t.TempDir()
	expiredCert, expiredKey := selfSigned(t, expiredDir, fixedNow.Add(-48*time.Hour), fixedNow.Add(-time.Hour), 0o600)
	looseDir := t.TempDir()
	looseCert, looseKey := selfSigned(t, looseDir, fixedNow.Add(-time.Hour), fixedNow.Add(time.Hour), 0o644)
	tokenFile := writeFile(t, filepath.Join(dir, "token"), []byte("tok\n"), 0o600)
	looseToken := writeFile(t, filepath.Join(dir, "loose-token"), []byte("tok\n"), 0o644)

	cases := []struct {
		name  string
		flags map[string]string
		env   map[string]string
		code  string // "" for success
		addr  string
		mode  string
	}{
		{name: "default loopback", flags: nil, addr: defaultAddr, mode: tlsModeLoopback},
		{name: "bare port", flags: map[string]string{"addr": "9000"}, addr: "127.0.0.1:9000", mode: tlsModeLoopback},
		{name: "colon port", flags: map[string]string{"addr": ":9000"}, addr: "127.0.0.1:9000", mode: tlsModeLoopback},
		{name: "remote needs allow-remote", flags: map[string]string{"addr": "0.0.0.0:7810"}, code: "config"},
		{name: "remote needs token", flags: map[string]string{"addr": "0.0.0.0:7810", "allow-remote": "true", "tls-terminated-upstream": "true"}, code: "config"},
		{name: "remote needs TLS", flags: map[string]string{"addr": "[::]:7810", "allow-remote": "true", "token-file": tokenFile}, code: "config"},
		{name: "remote with proxy TLS", flags: map[string]string{"addr": "0.0.0.0:7810", "allow-remote": "true", "token-file": tokenFile, "tls-terminated-upstream": "true"},
			addr: "0.0.0.0:7810", mode: tlsModeTerminated},
		{name: "remote with certificate", flags: map[string]string{"addr": "0.0.0.0:7810", "allow-remote": "true", "tls-cert-file": cert, "tls-key-file": key},
			env: map[string]string{"TRELLO_MCP_TOKEN": "tok"}, addr: "0.0.0.0:7810", mode: tlsModeHTTPS},
		{name: "certificate from env", env: map[string]string{"TRELLO_MCP_TLS_CERT_FILE": cert, "TRELLO_MCP_TLS_KEY_FILE": key}, addr: defaultAddr, mode: tlsModeHTTPS},
		{name: "terminated from env", env: map[string]string{"TRELLO_MCP_TLS_TERMINATED_UPSTREAM": "true"}, addr: defaultAddr, mode: tlsModeTerminated},
		{name: "bad terminated env", env: map[string]string{"TRELLO_MCP_TLS_TERMINATED_UPSTREAM": "yes"}, code: "config"},
		{name: "certificate and proxy", flags: map[string]string{"tls-cert-file": cert, "tls-key-file": key, "tls-terminated-upstream": "true"}, code: "usage"},
		{name: "certificate without key", flags: map[string]string{"tls-cert-file": cert}, code: "usage"},
		{name: "expired certificate", flags: map[string]string{"tls-cert-file": expiredCert, "tls-key-file": expiredKey}, code: "config"},
		{name: "key readable by others", flags: map[string]string{"tls-cert-file": looseCert, "tls-key-file": looseKey}, code: "config"},
		{name: "mismatched pair", flags: map[string]string{"tls-cert-file": cert, "tls-key-file": expiredKey}, code: "config"},
		{name: "token file readable by others", flags: map[string]string{"token-file": looseToken}, code: "config"},
		{name: "bad address", flags: map[string]string{"addr": "localhost:"}, code: "usage"},
	}
	for _, tc := range cases {
		application, _ := newApp(t, tc.env)
		flags := map[string]string{"transport": "http"}
		for key, value := range tc.flags {
			flags[key] = value
		}
		settings, err := resolveHTTP(application, &app.Invocation{Command: application.Registry.Get("serve"), Flags: flags})
		if tc.code != "" {
			if err == nil || string(err.Code) != tc.code {
				t.Errorf("%s: got %v, want %s", tc.name, err, tc.code)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if settings.addr != tc.addr || settings.tlsMode != tc.mode {
			t.Errorf("%s: addr %s mode %q", tc.name, settings.addr, settings.tlsMode)
		}
	}
}

func TestStdioRefusesHTTPFlags(t *testing.T) {
	application, _ := newApp(t, nil)
	response := Run(context.Background(), application, &app.Invocation{Command: application.Registry.Get("serve"),
		Flags: map[string]string{"addr": "127.0.0.1:9000"}})
	if response.Quiet || !strings.Contains(string(response.Document()), `"code":"usage"`) {
		t.Errorf("got %s", response.Document())
	}
}

// bearer adds the token to every request the MCP client makes.
type bearer struct{ token string }

func (b bearer) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(request)
}

func TestServeHTTPEndToEnd(t *testing.T) {
	application, stderr := newApp(t, map[string]string{"TRELLO_MCP_TOKEN": "tok"})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *app.Response, 1)
	go func() {
		done <- Run(ctx, application, &app.Invocation{Command: application.Registry.Get("serve"),
			Flags: map[string]string{"transport": "http", "addr": "127.0.0.1:0"}})
	}()

	var started map[string]any
	deadline := time.Now().Add(5 * time.Second)
	for started == nil && time.Now().Before(deadline) {
		scanner := bufio.NewScanner(strings.NewReader(stderr.String()))
		for scanner.Scan() {
			var line map[string]any
			if json.Unmarshal(scanner.Bytes(), &line) == nil && line["msg"] == "serve started" {
				started = line
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if started == nil {
		t.Fatalf("no startup line: %s", stderr.String())
	}
	if started["auth"] != "on" || started["tls"] != tlsModeLoopback || started["transport"] != "http" {
		t.Errorf("startup line %v", started)
	}
	if strings.Contains(stderr.String(), "tok") {
		t.Error("the token reached stderr")
	}
	base := "http://" + started["addr"].(string)

	health, err := http.Get(base + "/health")
	if err != nil || health.StatusCode != 200 {
		t.Fatalf("health: %v %v", health, err)
	}
	health.Body.Close()

	transport := &mcp.StreamableClientTransport{Endpoint: base + "/mcp", HTTPClient: &http.Client{Transport: bearer{"tok"}}}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if tool.Name == "dataset.clear" || tool.Name == "list-config" {
			t.Errorf("%s must not be offered over HTTP", tool.Name)
		}
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "boards.list", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("boards.list over HTTP: %v %+v", err, result)
	}
	session.Close()

	unauthenticated, err := http.Post(base+"/mcp", "application/json", strings.NewReader("{}"))
	if err != nil || unauthenticated.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated: %v %v", unauthenticated, err)
	}
	unauthenticated.Body.Close()

	cancel()
	select {
	case response := <-done:
		if !response.Quiet || response.Exit != 0 {
			t.Errorf("shutdown response quiet=%v exit=%d", response.Quiet, response.Exit)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not stop")
	}
	if !strings.Contains(stderr.String(), "serve stopped") {
		t.Error("no shutdown line")
	}
}
