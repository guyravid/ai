package app

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/guyravid/ai/cli/tools/trello/internal/datasets"
	"github.com/guyravid/ai/cli/tools/trello/internal/envelope"
	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
	"github.com/guyravid/ai/cli/tools/trello/internal/registry"
	"github.com/guyravid/ai/cli/tools/trello/internal/shape"
)

func (s *session) openStore() (*datasets.Store, *errs.Error) {
	if s.store != nil {
		return s.store, nil
	}
	dir := s.settings.String("DATASET_DIR")
	if dir == "" {
		reason := "it cannot be determined"
		if s.settings.DatasetDirErr != nil {
			reason = s.settings.DatasetDirErr.Error()
		}
		return nil, errs.Configf("The dataset directory is not set and %s.", reason).
			WithHint(fmt.Sprintf("Pass --dataset-dir <path> or set %s_DATASET_DIR.", s.app.Build.Prefix))
	}
	store, err := datasets.Open(dir, s.settings.Duration("DATASET_TTL"), s.settings.Size("DATASET_TOTAL_BYTES"), s.app.Now)
	if err != nil {
		return nil, err
	}
	s.store = store
	return store, nil
}

// dataset runs the dataset commands (contract §10.3). None of them contacts upstream.
func (s *session) dataset() *Response {
	store, err := s.openStore()
	if err != nil {
		return s.fail(err)
	}
	name := s.invocation.Command.Name
	id, _ := s.invocation.Params["id"].(string)
	switch name {
	case "dataset.read":
		return s.datasetRead(store, id)
	case "dataset.list":
		return s.datasetList(store)
	case "dataset.stat":
		sidecar, err := store.Lookup(id)
		if err != nil {
			return s.fail(err)
		}
		store.Touch(sidecar)
		value, _ := shape.FromAny(sidecar)
		s.envelope.Data = value
		return s.finish(Encode(s.envelope, s.invocation.Bool("pretty")))
	case "dataset.rm":
		if s.invocation.Bool("dry-run") {
			if _, err := store.Lookup(id); err != nil {
				return s.fail(err)
			}
			s.envelope.Data = shape.NewObject(shape.Field{Key: "would_remove", Value: shape.String(id)})
			return s.finish(Encode(s.envelope, s.invocation.Bool("pretty")))
		}
		if err := store.Remove(id); err != nil {
			return s.fail(err)
		}
		s.envelope.Data = shape.NewObject(shape.Field{Key: "removed", Value: shape.String(id)})
		return s.finish(Encode(s.envelope, s.invocation.Bool("pretty")))
	case "dataset.clear":
		if s.invocation.Bool("dry-run") {
			s.envelope.Data = shape.NewObject(shape.Field{Key: "would_remove", Value: shape.Int(int64(len(store.List())))})
			return s.finish(Encode(s.envelope, s.invocation.Bool("pretty")))
		}
		if !s.invocation.Bool("confirm") {
			return s.fail(errs.New(errs.Refused, "dataset clear deletes every dataset and requires --confirm.").
				WithHint(s.app.Build.Tool+" dataset clear --confirm").
				WithDetail("reason", "confirmation_required").
				WithDetail("preview", map[string]any{"action": "delete every dataset", "count": len(store.List())}))
		}
		removed := store.Clear()
		s.envelope.Data = shape.NewObject(shape.Field{Key: "removed", Value: shape.Int(int64(removed))})
		return s.finish(Encode(s.envelope, s.invocation.Bool("pretty")))
	}
	return s.fail(errs.New(errs.Internal, "Unknown dataset command %s.", name))
}

func (s *session) datasetRead(store *datasets.Store, id string) *Response {
	offset := int64(0)
	var limitFromCursor, fieldsExpr string
	fieldsExpr = s.invocation.Flags["fields"]
	if text, ok := s.invocation.Flags["cursor"]; ok {
		if s.invocation.Has("offset") {
			return s.fail(errs.Usagef("--cursor and --offset cannot be combined."))
		}
		cursor, err := envelope.DecodeCursor(text, "dataset.read")
		if err != nil {
			return s.fail(err)
		}
		if id != "" && id != cursor.D {
			return s.fail(errs.Usagef("The cursor belongs to dataset %s, not %s.", cursor.D, id))
		}
		id = cursor.D
		if cursor.O != nil {
			offset = *cursor.O
		}
		limitFromCursor = cursor.Q["limit"]
		if fieldsExpr == "" {
			fieldsExpr = cursor.Q["fields"]
		}
	} else if id == "" {
		return s.fail(errs.Usagef("dataset read requires <id> or --cursor.").WithHint(s.app.Build.Tool + " dataset list"))
	}
	if text, ok := s.invocation.Flags["offset"]; ok {
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err != nil || parsed < 0 {
			return s.fail(errs.Usagef("--offset must be a whole number of zero or more."))
		}
		offset = parsed
	}
	sidecar, err := store.Lookup(id)
	if err != nil {
		return s.fail(err)
	}
	fields := sidecar.Fields
	if fieldsExpr != "" {
		narrowed, fieldsErr := shape.ResolveFields(fieldsExpr, sidecar.Fields, sidecar.Fields)
		if fieldsErr != nil {
			return s.fail(fieldsErr.WithHint("This dataset holds: " + strings.Join(sidecar.Fields, ",")))
		}
		fields = narrowed
	}
	limit := s.resolveLimit(s.invocation.Command.Limits, limitFromCursor)
	store.Touch(sidecar)
	return s.readDataset(store, sidecar, offset, limit, fields, false)
}

// readDataset returns part of a stored dataset as a normal list envelope. It serves both
// dataset read and the first part of an --all response.
func (s *session) readDataset(store *datasets.Store, sidecar *datasets.Sidecar, offset int64, limit int, fields []string, collected bool) *Response {
	records, err := store.Read(sidecar, offset, limit)
	if err != nil {
		return s.fail(errs.Configf("Dataset %s could not be read.", sidecar.ID))
	}
	total := sidecar.RecordCount
	page := envelope.Page{Limit: limit, Total: &total, TotalIsExact: true}
	cursorAt := func(index int) string {
		next := offset + int64(index)
		return envelope.EncodeCursor(envelope.Cursor{C: "dataset.read", D: sidecar.ID, O: &next,
			Q: map[string]string{"limit": strconv.Itoa(limit), "fields": strings.Join(fields, ",")}})
	}
	if end := offset + int64(len(records)); end < total {
		cursor := cursorAt(len(records))
		page.HasMore, page.NextCursor = true, &cursor
	}
	s.envelope.Meta.Sort = sidecar.Sort
	if sidecar.Window != nil {
		s.envelope.Meta.Window = &envelope.Window{Since: sidecar.Window.Since, Until: sidecar.Window.Until}
		if collected {
			s.envelope.Meta.Window.Source = windowSource(s.invocation)
		}
	}
	meta := &envelope.Dataset{
		ID: sidecar.ID, Complete: sidecar.Complete, Offset: offset,
		TTLSeconds: int64(s.settings.Duration("DATASET_TTL") / time.Second),
	}
	if sidecar.IncompleteReason != nil {
		meta.IncompleteReason = *sidecar.IncompleteReason
	}
	if collected {
		count, size := sidecar.RecordCount, sidecar.Bytes
		meta.RecordCount, meta.Bytes = &count, &size
		if !s.invocation.HideDatasetPath {
			meta.Path = store.Path(sidecar.ID)
			if s.invocation.Bool("deterministic") {
				meta.Path = filepath.Base(meta.Path)
			}
		}
	}
	// Fill in the summary as if every record fits, so fitting sees its full size; the values
	// only shrink afterwards if the byte cap drops records.
	meta.Returned = len(records)
	hint := func(next int64) string {
		return fmt.Sprintf("%s dataset read %s --offset %d --limit 100", s.app.Build.Tool, sidecar.ID, next)
	}
	if collected {
		meta.ReadHint = hint(offset + int64(len(records)))
	}
	s.envelope.Meta.Dataset = meta
	response := s.respondList(records, fields, page, cursorAt)
	if response.Envelope.Error != nil {
		return response
	}
	returned := s.envelope.Meta.Page.Count
	meta.Returned = returned
	meta.ReadHint = ""
	if collected && offset+int64(returned) < total {
		meta.ReadHint = hint(offset + int64(returned))
	}
	return s.finish(Encode(s.envelope, s.invocation.Bool("pretty")))
}

func windowSource(invocation *Invocation) string {
	if invocation.Has("since") || invocation.Has("until") {
		return "flag"
	}
	return "default"
}

func (s *session) datasetList(store *datasets.Store) *Response {
	sidecars := store.List()
	now := s.app.Now()
	records := make([]shape.Value, 0, len(sidecars))
	for _, sidecar := range sidecars {
		created, _ := time.Parse(time.RFC3339, sidecar.CreatedAt)
		value, _ := shape.FromAny(registry.DatasetSummary{
			ID: sidecar.ID, Command: sidecar.Command, RecordCount: sidecar.RecordCount, Bytes: sidecar.Bytes,
			Complete: sidecar.Complete, AgeSeconds: int64(now.Sub(created) / time.Second),
		})
		records = append(records, value)
	}
	shape.SortRecords(records, shape.ParseSort(s.invocation.Command.Sort))
	limit := s.resolveLimit(s.invocation.Command.Limits, "")
	offset := 0
	if text, ok := s.invocation.Flags["cursor"]; ok {
		cursor, err := envelope.DecodeCursor(text, "dataset.list")
		if err != nil {
			return s.fail(err)
		}
		if cursor.O != nil {
			offset = int(*cursor.O)
		}
	}
	offset = min(offset, len(records))
	end := min(offset+limit, len(records))
	total := int64(len(records))
	page := envelope.Page{Limit: limit, Total: &total, TotalIsExact: true}
	cursorAt := func(index int) string {
		next := int64(offset + index)
		return envelope.EncodeCursor(envelope.Cursor{C: "dataset.list", O: &next})
	}
	if end < len(records) {
		cursor := cursorAt(end - offset)
		page.HasMore, page.NextCursor = true, &cursor
	}
	s.envelope.Meta.Sort = s.invocation.Command.Sort
	return s.respondList(records[offset:end], s.invocation.Command.Fields.Default, page, cursorAt)
}
