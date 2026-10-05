package app

import (
	"os"
	"path/filepath"
	"testing"
)

func checkStatuses(t *testing.T, r result) map[string]string {
	t.Helper()
	statuses := map[string]string{}
	data, _ := r.doc["data"].(map[string]any)
	checks, _ := data["checks"].([]any)
	for _, item := range checks {
		entry := item.(map[string]any)
		statuses[entry["name"].(string)], _ = entry["status"].(string)
	}
	return statuses
}

// A credential whose file permissions cannot be verified is a warn, not a failure, so doctor must
// still test it with the authenticated read (contract §7.1).
func TestDoctorRunsAuthWhenCredentialsWarn(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	write := func(name, value string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	delete(h.env, "TRELLO_API_KEY")
	delete(h.env, "TRELLO_API_TOKEN")
	h.env["TRELLO_API_KEY_FILE"] = write("key", testKey)
	h.env["TRELLO_API_TOKEN_FILE"] = write("token", testToken)
	h.app.Build.GOOS = "windows"
	h.env["APPDATA"] = filepath.Join(dir, "appdata")
	h.env["LOCALAPPDATA"] = filepath.Join(dir, "localappdata")
	h.env["USERPROFILE"] = dir

	result := h.run("doctor")
	statuses := checkStatuses(t, result)
	if statuses["credentials"] != "warn" {
		t.Fatalf("credentials = %q, want warn: %s", statuses["credentials"], result.raw)
	}
	if statuses["auth"] != "pass" {
		t.Errorf("auth = %q, want pass: %s", statuses["auth"], result.raw)
	}
	if statuses["clock"] == "skip" {
		t.Errorf("clock was skipped: %s", result.raw)
	}
	if h.fake.count("GET /1/members/me") != 1 {
		t.Errorf("the authenticated read ran %d times, want 1", h.fake.count("GET /1/members/me"))
	}
}
