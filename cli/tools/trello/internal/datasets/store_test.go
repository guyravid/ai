package datasets

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
	"github.com/guyravid/ai/cli/tools/trello/internal/shape"
)

// clock is an injected, advanceable time source safe for concurrent use.
type clock struct{ nanos atomic.Int64 }

func newClock() *clock {
	c := &clock{}
	c.nanos.Store(time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC).UnixNano())
	return c
}

func (c *clock) now() time.Time             { return time.Unix(0, c.nanos.Load()).UTC() }
func (c *clock) advance(step time.Duration) { c.nanos.Add(int64(step)) }

func noRedact(line []byte) []byte { return line }

func openStore(t *testing.T, c *clock, totalBytes int64) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "datasets"), time.Hour, totalBytes, c.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store
}

func record(t *testing.T, index int, padding int) shape.Value {
	t.Helper()
	value, err := shape.Parse([]byte(fmt.Sprintf(`{"i":%d,"pad":"%s"}`, index, strings.Repeat("x", padding))))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func collect(t *testing.T, store *Store, id string, records, padding int) *Sidecar {
	t.Helper()
	writer, err := store.Create(Sidecar{ID: id, Tool: "trello", Command: "cards.list", Params: map[string]string{"board": "B"}}, noRedact)
	if err != nil {
		t.Fatal(err)
	}
	for index := range records {
		if err := writer.Append(record(t, index, padding)); err != nil {
			t.Fatal(err)
		}
	}
	sidecar, err := writer.Commit(true, "")
	if err != nil {
		t.Fatal(err)
	}
	return sidecar
}

func index(t *testing.T, value shape.Value) string {
	t.Helper()
	encoded := string(value.Marshal())
	start := strings.Index(encoded, `"i":`) + 4
	end := strings.IndexAny(encoded[start:], ",}")
	return encoded[start : start+end]
}

func TestReadUsesIndexAtAnyOffset(t *testing.T) {
	store := openStore(t, newClock(), 1<<30)
	sidecar := collect(t, store, NewID(true, "a"), 1000, 4)
	if len(sidecar.Index) != 4 || sidecar.RecordCount != 1000 {
		t.Fatalf("index %v, count %d", sidecar.Index, sidecar.RecordCount)
	}
	for _, offset := range []int64{0, 1, 255, 256, 257, 700, 995, 999, 1000, 5000} {
		records, err := store.Read(sidecar, offset, 5)
		if err != nil {
			t.Fatal(err)
		}
		want := min(5, max(0, 1000-int(offset)))
		if len(records) != want {
			t.Fatalf("offset %d: %d records, want %d", offset, len(records), want)
		}
		if want > 0 && index(t, records[0]) != fmt.Sprint(offset) {
			t.Errorf("offset %d starts at record %s", offset, index(t, records[0]))
		}
	}
}

func TestAppendRedacts(t *testing.T) {
	store := openStore(t, newClock(), 1<<30)
	id := NewID(true, "redact")
	writer, _ := store.Create(Sidecar{ID: id}, func(line []byte) []byte {
		return bytes.ReplaceAll(line, []byte("SECRET"), []byte("***REDACTED***"))
	})
	value, _ := shape.Parse([]byte(`{"token":"SECRET"}`))
	_ = writer.Append(value)
	if _, err := writer.Commit(true, ""); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(store.Path(id))
	if bytes.Contains(content, []byte("SECRET\"")) || !bytes.Contains(content, []byte("***REDACTED***")) {
		t.Errorf("stored %s", content)
	}
}

func TestAbortLeavesNothing(t *testing.T) {
	store := openStore(t, newClock(), 1<<30)
	writer, _ := store.Create(Sidecar{ID: NewID(true, "abort")}, noRedact)
	_ = writer.Append(record(t, 0, 1))
	writer.Abort()
	entries, _ := os.ReadDir(store.Dir)
	if len(entries) != 0 {
		t.Errorf("left %v", entries)
	}
}

func TestLookupRejectsNonIDs(t *testing.T) {
	store := openStore(t, newClock(), 1<<30)
	for _, id := range []string{"../../etc/passwd", "", "abc", strings.ToUpper(NewID(true, "x"))} {
		if _, err := store.Lookup(id); err == nil || err.Code != errs.CacheMiss {
			t.Errorf("%q: %v", id, err)
		}
	}
}

func TestExpiryIsSlidingAndRemovesFiles(t *testing.T) {
	c := newClock()
	store := openStore(t, c, 1<<30)
	sidecar := collect(t, store, NewID(true, "ttl"), 3, 1)

	c.advance(50 * time.Minute)
	store.Touch(sidecar)
	c.advance(50 * time.Minute)
	if _, err := store.Lookup(sidecar.ID); err != nil {
		t.Fatalf("a read 50 minutes ago keeps it alive: %v", err)
	}
	c.advance(61 * time.Minute)
	_, err := store.Lookup(sidecar.ID)
	if err == nil || err.Details["reason"] != "expired" || !strings.Contains(err.Hint, "--all") {
		t.Fatalf("got %v", err)
	}
	if _, statErr := os.Stat(store.Path(sidecar.ID)); !os.IsNotExist(statErr) {
		t.Error("expired data file was not removed")
	}
}

func TestCleanupEvictsLeastRecentlyReadUnderBudget(t *testing.T) {
	c := newClock()
	// Each dataset is about 100 KB; a 250 KB budget holds two, and eviction goes to 90% (225 KB).
	store := openStore(t, c, 250_000)
	var ids []string
	for n := range 3 {
		ids = append(ids, collect(t, store, NewID(true, fmt.Sprint(n)), 100, 1000).ID)
		c.advance(time.Minute)
	}
	// Reading the oldest makes the middle one least recently read.
	oldest, _ := store.Lookup(ids[0])
	store.Touch(oldest)

	if err := store.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Lookup(ids[1]); err == nil {
		t.Error("the least recently read dataset should be evicted")
	}
	for _, id := range []string{ids[0], ids[2]} {
		if _, err := store.Lookup(id); err != nil {
			t.Errorf("%s should survive: %v", id[:8], err)
		}
	}
	if total := store.totalBytes(); float64(total) > evictTarget*float64(store.TotalBytes) {
		t.Errorf("total %d still above 90%% of %d", total, store.TotalBytes)
	}
}

func TestCleanupIsRateLimitedUnlessOverBudget(t *testing.T) {
	c := newClock()
	store := openStore(t, c, 1<<30)
	stale := collect(t, store, NewID(true, "stale"), 1, 1)
	c.advance(59*time.Minute + 50*time.Second)
	if err := store.Cleanup(); err != nil { // runs, leaves the marker
		t.Fatal(err)
	}
	c.advance(30 * time.Second) // stale has now expired, but the marker is 30s old
	if err := store.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.Path(stale.ID)); err != nil {
		t.Fatal("cleanup within a minute of the last one should not run")
	}
	store.TotalBytes = 1 // over budget: runs regardless of the marker
	if err := store.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.Path(stale.ID)); !os.IsNotExist(err) {
		t.Error("an over-budget cleanup must run despite a fresh marker")
	}
}

func TestStaleCleanupLockIsBroken(t *testing.T) {
	c := newClock()
	store := openStore(t, c, 1)
	collect(t, store, NewID(true, "big"), 10, 100)
	lock := filepath.Join(store.Dir, ".cleanup.lock")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := c.now().Add(-2 * lockStaleAfter)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	if err := store.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if len(store.List()) != 0 {
		t.Error("cleanup should have run after breaking the stale lock")
	}
}

// Readers of one dataset never see a partial file or sidecar while other goroutines write new
// datasets and touch the one being read. In serve mode these all share one process.
func TestConcurrentReadersAndWriters(t *testing.T) {
	c := newClock()
	store := openStore(t, c, 1<<30)
	shared := collect(t, store, NewID(true, "shared"), 600, 10)

	var wait sync.WaitGroup
	failures := make(chan string, 64)
	for writer := range 4 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for n := range 5 {
				id := NewID(true, fmt.Sprintf("w%d-%d", writer, n))
				created, err := store.Create(Sidecar{ID: id}, noRedact)
				if err != nil {
					failures <- fmt.Sprintf("writer %d create: %v", writer, err)
					return
				}
				for index := range 50 {
					value, _ := shape.Parse([]byte(fmt.Sprintf(`{"i":%d}`, index)))
					_ = created.Append(value)
				}
				sidecar, commitErr := created.Commit(true, "")
				if commitErr != nil {
					failures <- fmt.Sprintf("writer %d commit: %v", writer, commitErr)
					return
				}
				if records, err := store.Read(sidecar, 0, 100); err != nil || len(records) != 50 {
					failures <- fmt.Sprintf("writer %d read back %d records: %v", writer, len(records), err)
				}
			}
		}()
	}
	for reader := range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for n := range 25 {
				sidecar, err := store.Lookup(shared.ID)
				if err != nil {
					failures <- fmt.Sprintf("reader %d lookup %d: %v", reader, n, err)
					return
				}
				store.Touch(sidecar)
				records, readErr := store.Read(sidecar, int64(n*20), 20)
				if readErr != nil || len(records) != 20 || index(t, records[0]) != fmt.Sprint(n*20) {
					failures <- fmt.Sprintf("reader %d read %d: %d records, %v", reader, n, len(records), readErr)
					return
				}
			}
		}()
	}
	wait.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	if got := len(store.List()); got != 21 {
		t.Errorf("%d datasets listed, want 21", got)
	}
}
