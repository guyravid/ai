package app

import (
	"strings"
	"testing"

	"github.com/guyravid/ai/cli/tools/trello/internal/commands"
)

func recordIDs(r result) []string {
	var ids []string
	for _, item := range r.data() {
		ids = append(ids, item.(map[string]any)["id"].(string))
	}
	return ids
}

func TestCardAttachments(t *testing.T) {
	h := newHarness(t)
	listed := h.run("cards", "attachments", "c1")
	if listed.exit != 0 || strings.Join(recordIDs(listed), ",") != "at1,at2" {
		t.Fatalf("exit %d: %s", listed.exit, listed.raw)
	}
	first := listed.data()[0].(map[string]any)
	if first["isUpload"] != false || first["name"] != "Design" {
		t.Errorf("default fields not shaped as expected: %v", first)
	}
	if _, present := first["mimeType"]; present {
		t.Errorf("mimeType is not a default field: %v", first)
	}
	wide := h.run("cards", "attachments", "c1", "--fields", "id,bytes,mimeType")
	if wide.exit != 0 || wide.data()[1].(map[string]any)["bytes"] != float64(2048) {
		t.Errorf("--fields: %s", wide.raw)
	}
	missing := h.run("cards", "attachments", "missing")
	if missing.exit != 5 || missing.errorCode() != "not_found" {
		t.Errorf("missing card: %s", missing.raw)
	}
}

func TestCardComments(t *testing.T) {
	h := newHarness(t)
	listed := h.run("cards", "comments", "c1")
	if listed.exit != 0 || strings.Join(recordIDs(listed), ",") != "k2,k1" {
		t.Fatalf("exit %d: %s", listed.exit, listed.raw)
	}
	for _, want := range []string{"filter=commentCard", "limit=1000"} {
		if !strings.Contains(h.fake.lastQuery, want) {
			t.Errorf("query %q lacks %s", h.fake.lastQuery, want)
		}
	}
	newest := listed.data()[0].(map[string]any)
	member := newest["memberCreator"].(map[string]any)
	if newest["data"].(map[string]any)["text"] != "second" || member["username"] != "tester" || newest["date"] == nil {
		t.Errorf("record = %v", newest)
	}
	limited := h.run("cards", "comments", "c1", "--limit", "1")
	if len(limited.data()) != 1 || limited.page()["has_more"] != true {
		t.Errorf("limit: %s", limited.raw)
	}
	if missing := h.run("cards", "comments", "missing"); missing.errorCode() != "not_found" {
		t.Errorf("missing card: %s", missing.raw)
	}
}

func TestCardChecklists(t *testing.T) {
	h := newHarness(t)
	listed := h.run("cards", "checklists", "c1")
	if listed.exit != 0 || strings.Join(recordIDs(listed), ",") != "ck1,ck2" {
		t.Fatalf("exit %d: %s", listed.exit, listed.raw)
	}
	if !strings.Contains(h.fake.lastQuery, "checkItem_fields=name%2Cstate%2Cpos") {
		t.Errorf("query %q does not narrow check item fields", h.fake.lastQuery)
	}
	items := listed.data()[0].(map[string]any)["checkItems"].([]any)
	second := items[1].(map[string]any)
	if len(items) != 2 || second["id"] != "i2" || second["state"] != "incomplete" || second["pos"] != float64(32768) {
		t.Errorf("check items = %v", items)
	}
	if _, present := listed.data()[1].(map[string]any)["checkItems"]; present {
		t.Error("an empty checklist should have its empty checkItems stripped")
	}
	if missing := h.run("cards", "checklists", "missing"); missing.errorCode() != "not_found" {
		t.Errorf("missing card: %s", missing.raw)
	}
}

type cardWrite struct {
	name   string
	args   []string
	method string
	path   string
	body   string // substring expected in the confirmed request body; empty means none
}

var cardWrites = []cardWrite{
	{"cards.detach", []string{"cards", "detach", "c1", "--attachment", "at1"}, "DELETE", "/1/cards/c1/attachments/at1", ""},
	{"cards.add-checklist", []string{"cards", "add-checklist", "c1", "--name", "Release steps"}, "POST", "/1/cards/c1/checklists", `"name":"Release steps"`},
	{"checklists.add-item", []string{"checklists", "add-item", "ck1", "--name", "Tag the release", "--checked"}, "POST", "/1/checklists/ck1/checkItems", `"checked":"true"`},
	{"cards.check-item", []string{"cards", "check-item", "c1", "--check-item", "i1", "--state", "complete"}, "PUT", "/1/cards/c1/checkItem/i1", `"state":"complete"`},
}

func TestCardWritesRequireConfirm(t *testing.T) {
	if !commands.WritesEnabled {
		t.Skip("read-only build")
	}
	for _, tc := range cardWrites {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			refused := h.run(tc.args...)
			if refused.exit != 8 || refused.errorCode() != "refused" || h.fake.total() != 0 {
				t.Fatalf("without --confirm: exit %d, %d requests", refused.exit, h.fake.total())
			}
			preview := refused.doc["error"].(map[string]any)["details"].(map[string]any)["preview"].(map[string]any)
			if preview["method"] != tc.method || !strings.HasSuffix(preview["url"].(string), tc.path) ||
				preview["headers"].(map[string]any)["Authorization"] != "***REDACTED***" {
				t.Errorf("preview = %v", preview)
			}
			dry := h.run(append(append([]string(nil), tc.args...), "--dry-run", "--confirm")...)
			if dry.exit != 0 || h.fake.total() != 0 {
				t.Fatalf("dry run: exit %d, %d requests", dry.exit, h.fake.total())
			}
			sent := h.run(append(append([]string(nil), tc.args...), "--confirm")...)
			if sent.exit != 0 || h.fake.count(tc.method+" "+tc.path) != 1 || h.fake.total() != 1 {
				t.Fatalf("confirmed: exit %d: %s", sent.exit, sent.raw)
			}
			if tc.body != "" && !strings.Contains(h.fake.lastBody, tc.body) {
				t.Errorf("body %q lacks %s", h.fake.lastBody, tc.body)
			}
		})
	}
}

func TestCardWriteNotFound(t *testing.T) {
	if !commands.WritesEnabled {
		t.Skip("read-only build")
	}
	h := newHarness(t)
	missing := h.run("cards", "detach", "c1", "--attachment", "nope", "--confirm")
	if missing.exit != 5 || missing.errorCode() != "not_found" {
		t.Errorf("detaching a missing attachment: %s", missing.raw)
	}
}

func TestCheckItemRejectsUnknownState(t *testing.T) {
	if !commands.WritesEnabled {
		t.Skip("read-only build")
	}
	h := newHarness(t)
	for _, args := range [][]string{
		{"cards", "check-item", "c1", "--check-item", "i1", "--state", "done", "--confirm"},
		{"cards", "check-item", "c1", "--check-item", "i1", "--confirm"},
	} {
		if result := h.run(args...); result.exit != 2 || result.errorCode() != "usage" {
			t.Errorf("%v: exit %d: %s", args, result.exit, result.raw)
		}
	}
	if h.fake.total() != 0 {
		t.Errorf("%d requests sent for invalid input", h.fake.total())
	}
}

func TestDetachIsDestructiveAndChecklistWritesAreNot(t *testing.T) {
	if !commands.WritesEnabled {
		t.Skip("read-only build")
	}
	h := newHarness(t)
	want := map[string]bool{"cards.detach": true, "cards.add-checklist": false, "checklists.add-item": false, "cards.check-item": false}
	for name, destructive := range want {
		described := h.run("describe", name)
		annotations := described.doc["data"].(map[string]any)["commands"].([]any)[0].(map[string]any)["annotations"].(map[string]any)
		if annotations["destructiveHint"] != destructive || annotations["readOnlyHint"] != false {
			t.Errorf("%s annotations = %v", name, annotations)
		}
	}
}

func TestCardRename(t *testing.T) {
	if !commands.WritesEnabled {
		t.Skip("read-only build")
	}
	args := []string{"cards", "rename", "c1", "--name", "Fix paging bug"}
	h := newHarness(t)
	refused := h.run(args...)
	if refused.exit != 8 || refused.errorCode() != "refused" || h.fake.total() != 0 {
		t.Fatalf("without --confirm: exit %d, %d requests", refused.exit, h.fake.total())
	}
	preview := refused.doc["error"].(map[string]any)["details"].(map[string]any)["preview"].(map[string]any)
	if preview["method"] != "PUT" || !strings.HasSuffix(preview["url"].(string), "/1/cards/c1") {
		t.Errorf("preview = %v", preview)
	}
	if dry := h.run(append(args, "--dry-run", "--confirm")...); dry.exit != 0 || h.fake.total() != 0 {
		t.Fatalf("dry run: exit %d, %d requests", dry.exit, h.fake.total())
	}
	sent := h.run(append(args, "--confirm")...)
	if sent.exit != 0 || h.fake.count("PUT /1/cards/c1") != 1 || !strings.Contains(h.fake.lastBody, `"name":"Fix paging bug"`) {
		t.Fatalf("confirmed: exit %d, body %s", sent.exit, h.fake.lastBody)
	}
	if sent.doc["data"].(map[string]any)["id"] != "c1" {
		t.Errorf("data = %v", sent.doc["data"])
	}
	before := h.fake.total()
	if empty := h.run("cards", "rename", "c1", "--name", "", "--confirm"); empty.exit != 2 || empty.errorCode() != "usage" {
		t.Errorf("empty name is reported as a missing required flag: exit %d: %s", empty.exit, empty.raw)
	}
	for _, name := range []string{"  ", "two\nlines"} {
		result := h.run("cards", "rename", "c1", "--name", name, "--confirm")
		if result.exit != 6 || result.errorCode() != "validation" {
			t.Errorf("name %q: exit %d: %s", name, result.exit, result.raw)
		}
	}
	if h.fake.total() != before {
		t.Error("requests sent for an invalid name")
	}
	if missing := h.run("cards", "rename", "nope", "--name", "x", "--confirm"); missing.errorCode() != "not_found" {
		t.Errorf("missing card: %s", missing.raw)
	}
}
