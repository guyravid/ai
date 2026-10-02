// Package render turns a finished envelope into plain text for --human (contract §15). It reads
// only the envelope, so it can never report a different outcome from the JSON.
package render

import (
	"fmt"
	"strings"

	"github.com/guyravid/ai/cli/tools/trello/internal/envelope"
	"github.com/guyravid/ai/cli/tools/trello/internal/shape"
)

const maxCell = 60

// Human renders an envelope. No colour, no escapes, no terminal detection.
func Human(e *envelope.Envelope) string {
	var builder strings.Builder
	if e.Error != nil {
		fmt.Fprintf(&builder, "ERROR %s (exit %d): %s\n", e.Error.Code, e.Error.ExitCode, e.Error.Message)
		if e.Error.Hint != "" {
			fmt.Fprintf(&builder, "hint: %s\n", e.Error.Hint)
		}
		if e.Error.Code == "refused" {
			if preview, ok := e.Error.Details["preview"]; ok {
				fmt.Fprintf(&builder, "preview: %s\n", compact(preview))
			}
		}
	}
	command := ""
	if e.Command != nil {
		command = *e.Command
	}
	switch {
	case command == "doctor" && e.Data.Kind == shape.Object:
		builder.WriteString(doctor(e.Data))
	case command == "list-config" && e.Data.Kind == shape.Object:
		builder.WriteString(listConfig(e.Data))
	case e.Data.Kind == shape.Array:
		builder.WriteString(list(e.Data, e.Meta))
	case e.Data.Kind == shape.Object:
		builder.WriteString(object(e.Data))
	}
	for _, warning := range e.Meta.Warnings {
		fmt.Fprintf(&builder, "warning: %s: %s\n", warning.Code, warning.Message)
	}
	if e.Meta.Dataset != nil {
		fmt.Fprintf(&builder, "dataset: %s (%d returned from offset %d, complete: %t)\n",
			e.Meta.Dataset.ID, e.Meta.Dataset.Returned, e.Meta.Dataset.Offset, e.Meta.Dataset.Complete)
	}
	if builder.Len() == 0 {
		builder.WriteString("ok\n")
	}
	return builder.String()
}

func doctor(data shape.Value) string {
	checks, _ := data.Get("checks")
	var rows [][]string
	for _, item := range checks.Items {
		status := text(field(item, "status"))
		if status == "fail" {
			status = "FAIL"
		}
		rows = append(rows, []string{status, text(field(item, "name")), text(field(item, "detail"))})
	}
	return table(nil, rows)
}

func listConfig(data shape.Value) string {
	var builder strings.Builder
	if profile := field(data, "profile"); !profile.IsNull() {
		fmt.Fprintf(&builder, "profile: %s\n", text(profile))
	}
	if file := field(data, "config_file"); !file.IsNull() {
		fmt.Fprintf(&builder, "config file: %s\n", text(file))
	}
	settings, _ := data.Get("settings")
	var rows [][]string
	for _, item := range settings.Items {
		value := text(field(item, "value"))
		if set, ok := item.Get("set"); ok {
			value = "not set"
			if string(set.Raw) == "true" {
				value = "set"
			}
		}
		rows = append(rows, []string{text(field(item, "name")), value, text(field(item, "source"))})
	}
	builder.WriteString(table([]string{"SETTING", "VALUE", "SOURCE"}, rows))
	return builder.String()
}

func list(data shape.Value, meta envelope.Meta) string {
	if len(data.Items) > 0 && data.Items[0].Kind != shape.Object {
		var builder strings.Builder
		for _, item := range data.Items {
			builder.WriteString(text(item) + "\n")
		}
		return builder.String()
	}
	columns := meta.Fields
	if len(columns) == 0 {
		seen := map[string]bool{}
		for _, item := range data.Items {
			for _, member := range item.Fields {
				if !seen[member.Key] {
					seen[member.Key] = true
					columns = append(columns, member.Key)
				}
			}
		}
	}
	var rows [][]string
	for _, item := range data.Items {
		row := make([]string, len(columns))
		for index, column := range columns {
			value, ok := item.Lookup(strings.Split(strings.TrimSuffix(column, ".*"), "."))
			if ok {
				row[index] = cell(value)
			}
		}
		rows = append(rows, row)
	}
	headers := make([]string, len(columns))
	for index, column := range columns {
		headers[index] = strings.ToUpper(column)
	}
	var builder strings.Builder
	builder.WriteString(table(headers, rows))
	if meta.Page != nil {
		more := "no more"
		if meta.Page.HasMore {
			more = "more available (pass meta.page.next_cursor to --cursor)"
		}
		fmt.Fprintf(&builder, "%d rows; %s\n", meta.Page.Count, more)
	}
	return builder.String()
}

func object(data shape.Value) string {
	width := 0
	for _, member := range data.Fields {
		width = max(width, len(member.Key))
	}
	var builder strings.Builder
	for _, member := range data.Fields {
		fmt.Fprintf(&builder, "%-*s  %s\n", width+1, member.Key+":", text(member.Value))
	}
	return builder.String()
}

func table(headers []string, rows [][]string) string {
	columns := len(headers)
	for _, row := range rows {
		columns = max(columns, len(row))
	}
	widths := make([]int, columns)
	measure := func(row []string) {
		for index, value := range row {
			widths[index] = max(widths[index], len([]rune(value)))
		}
	}
	measure(headers)
	for _, row := range rows {
		measure(row)
	}
	var builder strings.Builder
	write := func(row []string) {
		for index := 0; index < columns; index++ {
			value := ""
			if index < len(row) {
				value = row[index]
			}
			if index == columns-1 {
				builder.WriteString(value)
			} else {
				builder.WriteString(value + strings.Repeat(" ", widths[index]-len([]rune(value))+2))
			}
		}
		builder.WriteString("\n")
	}
	if len(headers) > 0 {
		write(headers)
	}
	for _, row := range rows {
		write(row)
	}
	return builder.String()
}

func field(value shape.Value, key string) shape.Value {
	member, ok := value.Get(key)
	if !ok {
		return shape.Null()
	}
	return member
}

// text is a readable form of any value: strings unquoted, everything else as compact JSON.
func text(value shape.Value) string {
	if value.IsString() {
		return strings.ReplaceAll(value.Text(), "\n", " ")
	}
	if value.IsNull() {
		return ""
	}
	return string(value.Marshal())
}

func cell(value shape.Value) string {
	rendered := text(value)
	if runes := []rune(rendered); len(runes) > maxCell {
		return string(runes[:maxCell-1]) + "…"
	}
	return rendered
}

func compact(value any) string {
	converted, err := shape.FromAny(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(converted.Marshal())
}
