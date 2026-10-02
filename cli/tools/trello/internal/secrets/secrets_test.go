package secrets

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guyravid/ai/cli/tools/trello/internal/config"
	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
)

func envOf(values map[string]string) config.Env {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func secretFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func resolveKey(t *testing.T, profile string, env map[string]string, file *config.File) (*Credential, *errs.Error) {
	t.Helper()
	credentials, _, err := Resolve(Input{Prefix: "TRELLO", Profile: profile, Env: envOf(env), GOOS: "linux",
		File: file, Names: []string{"API_KEY"}})
	if err != nil {
		return nil, err
	}
	return credentials[0], nil
}

func TestPrecedence(t *testing.T) {
	file := secretFile(t, "from-file-value\n", 0o600)
	cases := []struct {
		name    string
		profile string
		env     map[string]string
		want    string
		source  string
	}{
		{"direct beats file in scope", "", map[string]string{"TRELLO_API_KEY": "direct-value", "TRELLO_API_KEY_FILE": file}, "direct-value", "TRELLO_API_KEY"},
		{"file variable", "", map[string]string{"TRELLO_API_KEY_FILE": file}, "from-file-value", "TRELLO_API_KEY_FILE"},
		{"profile beats unscoped", "work", map[string]string{"TRELLO_API_KEY": "unscoped", "TRELLO_API_KEY_FILE_WORK": file}, "from-file-value", "TRELLO_API_KEY_FILE_WORK"},
		{"profile direct first", "work", map[string]string{"TRELLO_API_KEY_WORK": "scoped", "TRELLO_API_KEY_FILE_WORK": file}, "scoped", "TRELLO_API_KEY_WORK"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			credential, err := resolveKey(t, tc.profile, tc.env, nil)
			if err != nil {
				t.Fatal(err)
			}
			if credential.Secret.Reveal() != tc.want || credential.Source != tc.source {
				t.Errorf("got %q from %s", credential.Secret.Reveal(), credential.Source)
			}
		})
	}
}

func TestPermissiveFileIsConfigError(t *testing.T) {
	path := secretFile(t, "value-in-open-file", 0o644)
	_, err := resolveKey(t, "", map[string]string{"TRELLO_API_KEY_FILE": path}, nil)
	if err == nil || err.Code != errs.Config || !strings.Contains(err.Hint, "chmod 600") {
		t.Fatalf("got %v", err)
	}
}

func TestNamedButUnreadableFileDoesNotFallThrough(t *testing.T) {
	_, err := resolveKey(t, "", map[string]string{"TRELLO_API_KEY_FILE": "/nonexistent", "TRELLO_API_KEY_FILE_X": "ignored"}, nil)
	if err == nil || err.Code != errs.Config {
		t.Fatalf("got %v", err)
	}
}

func TestEnvFileProfileKeyFirst(t *testing.T) {
	envFile := secretFile(t, "# comment\nexport TRELLO_API_KEY=plain-key-value\nTRELLO_API_KEY_WORK=\"work-key-value\"\nOTHER=zzz\n", 0o600)
	configPath := filepath.Join(t.TempDir(), "config.json")
	content := fmt.Sprintf(`{"config_version":1,"default":{"env_file":%q},"profiles":{"work":{}}}`, envFile)
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	file, loadErr := config.LoadFile(configPath, true, config.Defs(), []string{"API_KEY"})
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	credential, err := resolveKey(t, "work", map[string]string{"HOME": t.TempDir()}, file)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Secret.Reveal() != "work-key-value" || credential.Origin != config.OriginFileDefault {
		t.Errorf("got %q (%s)", credential.Secret.Reveal(), credential.Origin)
	}
	credential, _ = resolveKey(t, "", map[string]string{"HOME": t.TempDir()}, file)
	if credential.Secret.Reveal() != "plain-key-value" {
		t.Errorf("got %q", credential.Secret.Reveal())
	}
}

func TestMissingIsNotAnError(t *testing.T) {
	credential, err := resolveKey(t, "", map[string]string{}, nil)
	if err != nil || credential.Set {
		t.Fatalf("got %+v, %v", credential, err)
	}
}

func TestSecretNeverPrints(t *testing.T) {
	secret := NewSecret("do-not-print-me")
	for _, text := range []string{fmt.Sprint(secret), fmt.Sprintf("%v %+v %#v %s", secret, secret, secret, secret)} {
		if strings.Contains(text, "do-not-print-me") {
			t.Fatalf("printed: %s", text)
		}
	}
}

func TestRedactorForms(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef"
	token := "tok/en+with=special&chars-0123456789"
	header := `OAuth oauth_consumer_key="` + key + `", oauth_token="` + token + `"`
	redactor := NewRedactor([]string{key, token, "short"}, []string{header})

	inputs := []string{
		key,
		token,
		url.QueryEscape(token),
		base64.StdEncoding.EncodeToString([]byte(key)),
		base64.RawURLEncoding.EncodeToString([]byte(token)),
		header,
		key[:12],
		key[:20] + "-truncated-echo",
	}
	for _, input := range inputs {
		output := redactor.String("before " + input + " after")
		if strings.Contains(output, key[:12]) || strings.Contains(output, token[:12]) {
			t.Errorf("not redacted: %q -> %q", input, output)
		}
		if !strings.Contains(output, Marker) {
			t.Errorf("no marker: %q", output)
		}
	}
	if redactor.String("short and sweet") != "short and sweet" {
		t.Error("a credential under 8 characters must not be registered")
	}
}
