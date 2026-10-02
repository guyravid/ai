package teach

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guyravid/ai/cli/tools/telegram/internal/config"
)

// The vendored templates must be byte-identical to the bundle's copies (base template §14).
func TestVendoredTemplatesMatchBundle(t *testing.T) {
	for _, name := range []string{"orientation.md.tmpl", "shared.md.tmpl"} {
		bundle, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "base", "teach", name))
		if err != nil {
			t.Skipf("bundle copy not found (%v); run inside the cli bundle", err)
		}
		vendored, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if string(bundle) != string(vendored) {
			t.Errorf("%s differs from the bundle; re-vendor with: cp ../../../../base/teach/%s internal/teach/", name, name)
		}
	}
}

func TestOrientationUnder4KB(t *testing.T) {
	for _, writes := range []bool{true, false} {
		text, err := Render(Input{Data: Data{Tool: "telegram", Contract: "1.4.2", Prefix: "TELEGRAM", HasDatasets: false,
			WritesEnabled: writes, Domain: Domain{Summary: strings.Repeat("x", 400)}}}, false, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(text) >= 4096 {
			t.Errorf("orientation is %d bytes", len(text))
		}
	}
}

func TestTopicsAreNotReserved(t *testing.T) {
	topics, err := domainTopics(true)
	if err != nil {
		t.Fatal(err)
	}
	for _, topic := range topics {
		if reservedTopics[topic.name] || topic.summary == "" {
			t.Errorf("topic %q is reserved or has no summary", topic.name)
		}
	}
}

// Contract §6.3: a tool that supports profile descriptions explains them in the profiles aspect, and
// its example config must itself be a valid config file.
func TestConfigProfilesAspectExplainsDescriptions(t *testing.T) {
	text := configProfiles(Input{Data: Data{Tool: "telegram", Prefix: "TELEGRAM"}})
	for _, want := range []string{"`description`", "list-profiles", "not a setting", "200"} {
		if !strings.Contains(text, want) {
			t.Errorf("the profiles aspect should mention %q:\n%s", want, text)
		}
	}
}

func TestExampleConfigIsValidAndDescribesEveryProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(ExampleConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := config.LoadFile(path, true, config.Defs(), []string{"BOT_TOKEN"})
	if err != nil {
		t.Fatalf("the example config does not load: %s", err.Message)
	}
	if file.Description("") == nil {
		t.Errorf("the example default section has no description")
	}
	if len(file.Profiles) == 0 {
		t.Fatal("the example config declares no profiles")
	}
	for name := range file.Profiles {
		if file.Description(name) == nil {
			t.Errorf("example profile %s has no description", name)
		}
	}
}

func TestApplyWritesKeepsTheBlockOnlyWithWrites(t *testing.T) {
	body := "before\n{{#writes}}\nsecret send\n{{/writes}}\nafter"
	if got := applyWrites(body, true); got != "before\nsecret send\nafter" {
		t.Errorf("with writes: %q", got)
	}
	if got := applyWrites(body, false); got != "before\nafter" {
		t.Errorf("read-only: %q", got)
	}
}
