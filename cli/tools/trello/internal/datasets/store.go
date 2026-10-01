// Package datasets stores complete result sets collected with --all as JSON Lines, and serves them
// back in parts without contacting upstream (contract §10).
package datasets

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
	"github.com/guyravid/ai/cli/tools/trello/internal/shape"
)

const (
	indexStride     = 256
	cleanupInterval = 60 * time.Second
	lockStaleAfter  = 60 * time.Second
	evictTarget     = 0.9
)

var idPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Sidecar is <id>.meta.json. Its presence proves the data file is complete (base template §10.3).
type Sidecar struct {
	ID               string            `json:"id"`
	Tool             string            `json:"tool"`
	Command          string            `json:"command"`
	Params           map[string]string `json:"params"`
	Fields           []string          `json:"fields"`
	Sort             string            `json:"sort"`
	Window           *Window           `json:"window"`
	CreatedAt        string            `json:"created_at"`
	LastAccessedAt   string            `json:"last_accessed_at"`
	Complete         bool              `json:"complete"`
	IncompleteReason *string           `json:"incomplete_reason"`
	RecordCount      int64             `json:"record_count"`
	Bytes            int64             `json:"bytes"`
	IndexStride      int               `json:"index_stride"`
	Index            []int64           `json:"index"`
	ContractVersion  string            `json:"contract_version"`
	ToolVersion      string            `json:"tool_version"`
}

type Window struct {
	Since string `json:"since"`
	Until string `json:"until"`
}

type Store struct {
	Dir        string
	TTL        time.Duration
	TotalBytes int64
	Now        func() time.Time
	root       *os.Root
}

// Open creates the directory owner-only if needed. Any failure is config: the tool never falls back
// to storing a dataset somewhere else (contract §10.2).
func Open(dir string, ttl time.Duration, totalBytes int64, now func() time.Time) (*Store, *errs.Error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, unusable(dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, unusable(dir, err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, unusable(dir, err)
	}
	probe := fmt.Sprintf(".probe-%d", os.Getpid())
	file, err := root.OpenFile(probe, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		root.Close()
		return nil, unusable(dir, err)
	}
	file.Close()
	_ = root.Remove(probe)
	return &Store{Dir: dir, TTL: ttl, TotalBytes: totalBytes, Now: now, root: root}, nil
}

func unusable(dir string, err error) *errs.Error {
	return errs.Configf("The dataset directory %s cannot be created or written.", dir).
		WithHint("Set --dataset-dir or TRELLO_DATASET_DIR to a writable directory.").
		WithDetail("dataset_dir", dir).WithDetail("reason", rootCause(err))
}

func rootCause(err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err.Error()
	}
	return err.Error()
}

func (s *Store) Close() { s.root.Close() }

// NewID returns a random UUIDv4, or with deterministic set, one derived from the request key.
func NewID(deterministic bool, key string) string {
	var raw [16]byte
	if deterministic {
		sum := sha256.Sum256([]byte(key))
		copy(raw[:], sum[:16])
	} else {
		_, _ = rand.Read(raw[:])
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}

func (s *Store) Path(id string) string { return filepath.Join(s.Dir, id+".jsonl") }

// Writer streams records into a temporary file, then renames it into place on Commit.
type Writer struct {
	store    *Store
	sidecar  Sidecar
	file     *os.File
	buffer   *bufio.Writer
	redact   func([]byte) []byte
	offset   int64
	finished bool
}

// Create starts a dataset. Everything written passes through redact.
func (s *Store) Create(sidecar Sidecar, redact func([]byte) []byte) (*Writer, *errs.Error) {
	file, err := s.root.OpenFile(sidecar.ID+".jsonl.tmp", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, unusable(s.Dir, err)
	}
	now := s.Now().UTC().Format(time.RFC3339)
	sidecar.CreatedAt, sidecar.LastAccessedAt, sidecar.IndexStride = now, now, indexStride
	sidecar.Index = []int64{}
	return &Writer{store: s, sidecar: sidecar, file: file, buffer: bufio.NewWriterSize(file, 256<<10), redact: redact}, nil
}

func (w *Writer) Records() int64 { return w.sidecar.RecordCount }
func (w *Writer) Bytes() int64   { return w.offset }

// Append writes one record as one line.
func (w *Writer) Append(record shape.Value) error {
	line := w.redact(record.Marshal())
	if w.sidecar.RecordCount%indexStride == 0 {
		w.sidecar.Index = append(w.sidecar.Index, w.offset)
	}
	if _, err := w.buffer.Write(line); err != nil {
		return err
	}
	if err := w.buffer.WriteByte('\n'); err != nil {
		return err
	}
	w.offset += int64(len(line)) + 1
	w.sidecar.RecordCount++
	return nil
}

// Commit syncs and renames the data file, then writes the sidecar the same way.
func (w *Writer) Commit(complete bool, reason string) (*Sidecar, *errs.Error) {
	w.finished = true
	fail := func(err error) (*Sidecar, *errs.Error) {
		w.file.Close()
		_ = w.store.root.Remove(w.sidecar.ID + ".jsonl.tmp")
		return nil, unusable(w.store.Dir, err)
	}
	if err := w.buffer.Flush(); err != nil {
		return fail(err)
	}
	if err := w.file.Sync(); err != nil {
		return fail(err)
	}
	if err := w.file.Close(); err != nil {
		return fail(err)
	}
	if err := w.store.root.Rename(w.sidecar.ID+".jsonl.tmp", w.sidecar.ID+".jsonl"); err != nil {
		return fail(err)
	}
	w.sidecar.Complete = complete
	if !complete {
		w.sidecar.IncompleteReason = &reason
	}
	w.sidecar.Bytes = w.offset
	if err := w.store.writeSidecar(&w.sidecar); err != nil {
		_ = w.store.root.Remove(w.sidecar.ID + ".jsonl")
		return nil, unusable(w.store.Dir, err)
	}
	return &w.sidecar, nil
}

// Abort discards an unfinished dataset.
func (w *Writer) Abort() {
	if w.finished {
		return
	}
	w.finished = true
	w.file.Close()
	_ = w.store.root.Remove(w.sidecar.ID + ".jsonl.tmp")
}

func (s *Store) writeSidecar(sidecar *Sidecar) error {
	encoded, err := json.Marshal(sidecar)
	if err != nil {
		return err
	}
	temporary := fmt.Sprintf("%s.meta.json.%d.tmp", sidecar.ID, os.Getpid())
	file, err := s.root.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(encoded); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return s.root.Rename(temporary, sidecar.ID+".meta.json")
}

// Lookup loads a sidecar, removing the dataset if it has expired. It never resets the lifetime.
func (s *Store) Lookup(id string) (*Sidecar, *errs.Error) {
	if !idPattern.MatchString(id) {
		return nil, s.miss(id, nil, "unknown")
	}
	content, err := s.root.ReadFile(id + ".meta.json")
	if err != nil {
		return nil, s.miss(id, nil, "unknown")
	}
	var sidecar Sidecar
	if err := json.Unmarshal(content, &sidecar); err != nil {
		return nil, s.miss(id, nil, "unknown")
	}
	if s.expired(&sidecar) {
		s.remove(id)
		return nil, s.miss(id, &sidecar, "expired")
	}
	return &sidecar, nil
}

// Touch resets a dataset's lifetime (contract §10.2, sliding lifetime).
func (s *Store) Touch(sidecar *Sidecar) {
	sidecar.LastAccessedAt = s.Now().UTC().Format(time.RFC3339)
	_ = s.writeSidecar(sidecar)
}

func (s *Store) expired(sidecar *Sidecar) bool {
	last, err := time.Parse(time.RFC3339, sidecar.LastAccessedAt)
	return err != nil || s.Now().Sub(last) > s.TTL
}

func (s *Store) miss(id string, sidecar *Sidecar, reason string) *errs.Error {
	short := id
	if len(short) > 8 {
		short = short[:8]
	}
	message := fmt.Sprintf("No dataset %s exists; it may never have existed or may have been removed.", short)
	hint := "Re-run the original query with --all to collect it again."
	if reason == "expired" {
		message = fmt.Sprintf("Dataset %s expired; datasets live %s after their last read.", short, formatDuration(s.TTL))
	}
	if sidecar != nil {
		hint = RerunHint(sidecar)
	}
	return errs.New(errs.CacheMiss, "%s", message).WithHint(hint).WithDetail("id", id).WithDetail("reason", reason)
}

// RerunHint names the query that rebuilds a dataset.
func RerunHint(sidecar *Sidecar) string {
	parts := append([]string{sidecar.Tool}, strings.Split(sidecar.Command, ".")...)
	keys := make([]string, 0, len(sidecar.Params))
	for key := range sidecar.Params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := sidecar.Params[key]
		if strings.ContainsAny(value, " \"'") {
			value = fmt.Sprintf("%q", value)
		}
		if strings.HasPrefix(key, "<") {
			parts = append(parts, value)
		} else {
			parts = append(parts, "--"+strings.ReplaceAll(key, "_", "-"), value)
		}
	}
	return strings.Join(append(parts, "--all"), " ")
}

func formatDuration(duration time.Duration) string {
	if duration%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(duration.Hours()))
	}
	return duration.String()
}

// Read returns up to limit records starting at offset, as stored.
func (s *Store) Read(sidecar *Sidecar, offset int64, limit int) ([]shape.Value, error) {
	file, err := s.root.Open(sidecar.ID + ".jsonl")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	line := int64(0)
	if len(sidecar.Index) > 0 && offset > 0 {
		slot := min(offset/int64(sidecar.IndexStride), int64(len(sidecar.Index)-1))
		if _, err := file.Seek(sidecar.Index[slot], io.SeekStart); err != nil {
			return nil, err
		}
		line = slot * int64(sidecar.IndexStride)
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 16<<20)
	var records []shape.Value
	for scanner.Scan() && len(records) < limit {
		if line >= offset {
			record, err := shape.Parse(bytes.Clone(scanner.Bytes()))
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		}
		line++
	}
	return records, scanner.Err()
}

// List returns every live dataset.
func (s *Store) List() []*Sidecar {
	entries, err := fs.ReadDir(s.root.FS(), ".")
	if err != nil {
		return nil
	}
	var sidecars []*Sidecar
	for _, entry := range entries {
		id, ok := strings.CutSuffix(entry.Name(), ".meta.json")
		if !ok {
			continue
		}
		if sidecar, err := s.Lookup(id); err == nil {
			sidecars = append(sidecars, sidecar)
		}
	}
	return sidecars
}

// Remove deletes one dataset, sidecar first, so a concurrent reader sees a clean cache_miss.
func (s *Store) Remove(id string) *errs.Error {
	if _, err := s.Lookup(id); err != nil {
		return err
	}
	s.remove(id)
	return nil
}

func (s *Store) remove(id string) {
	_ = s.root.Remove(id + ".meta.json")
	_ = s.root.Remove(id + ".jsonl")
}

// Clear deletes every dataset and returns how many were removed.
func (s *Store) Clear() int {
	sidecars := s.List()
	for _, sidecar := range sidecars {
		s.remove(sidecar.ID)
	}
	return len(sidecars)
}

// Cleanup removes expired datasets, then evicts the least recently read until the directory is
// under 90% of its budget. It runs after the response is written and never fails a command.
func (s *Store) Cleanup() error {
	total := s.totalBytes()
	if info, err := s.root.Stat(".cleanup"); err == nil && s.Now().Sub(info.ModTime()) < cleanupInterval && total <= s.TotalBytes {
		return nil
	}
	lock, err := s.root.OpenFile(".cleanup.lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return err
		}
		info, statErr := s.root.Stat(".cleanup.lock")
		if statErr != nil || s.Now().Sub(info.ModTime()) < lockStaleAfter {
			return nil
		}
		_ = s.root.Remove(".cleanup.lock")
		if lock, err = s.root.OpenFile(".cleanup.lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600); err != nil {
			return nil
		}
	}
	lock.Close()
	defer func() { _ = s.root.Remove(".cleanup.lock") }()

	live := s.List() // Lookup already removes expired datasets
	s.removeOrphans()
	sort.Slice(live, func(i, j int) bool { return live[i].LastAccessedAt < live[j].LastAccessedAt })
	total = s.totalBytes()
	for _, sidecar := range live {
		if float64(total) <= evictTarget*float64(s.TotalBytes) {
			break
		}
		s.remove(sidecar.ID)
		total -= sidecar.Bytes
	}
	marker, err := s.root.OpenFile(".cleanup", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	marker.Close()
	now := s.Now()
	return s.root.Chtimes(".cleanup", now, now)
}

// removeOrphans deletes data files without a sidecar and stale temporary files.
func (s *Store) removeOrphans() {
	entries, err := fs.ReadDir(s.root.FS(), ".")
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		info, err := entry.Info()
		if err != nil || s.Now().Sub(info.ModTime()) < s.TTL {
			continue
		}
		if strings.HasSuffix(name, ".tmp") {
			_ = s.root.Remove(name)
		}
		if id, ok := strings.CutSuffix(name, ".jsonl"); ok {
			if _, err := s.root.Stat(id + ".meta.json"); err != nil {
				_ = s.root.Remove(name)
			}
		}
	}
}

func (s *Store) totalBytes() int64 {
	var total int64
	entries, err := fs.ReadDir(s.root.FS(), ".")
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil && !entry.IsDir() {
			total += info.Size()
		}
	}
	return total
}
