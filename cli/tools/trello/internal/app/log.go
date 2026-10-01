package app

import (
	"encoding/json"
	"io"

	"github.com/guyravid/ai/cli/tools/trello/internal/secrets"
)

var levelRank = map[string]int{"off": 0, "error": 1, "warn": 2, "info": 3, "debug": 4}

// logger writes one redacted JSON object per line to stderr (base template §17).
type logger struct {
	out   io.Writer
	level int
}

func newLogger(stderr io.Writer, level string, redactor *secrets.Redactor) *logger {
	if stderr == nil {
		stderr = io.Discard
	}
	return &logger{out: redactor.Writer(stderr), level: levelRank[level]}
}

func (l *logger) write(level, message string, fields map[string]any) {
	if l == nil || levelRank[level] > l.level || levelRank[level] == 0 {
		return
	}
	entry := map[string]any{"level": level, "msg": message}
	for key, value := range fields {
		entry[key] = value
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = l.out.Write(append(line, '\n'))
}
