package teach

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		text, err := Render(Input{Data: Data{Tool: "trello", Contract: "1.1", Prefix: "TRELLO", HasDatasets: true,
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
