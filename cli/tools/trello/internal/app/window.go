package app

import (
	"regexp"
	"strconv"
	"time"

	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
	"github.com/guyravid/ai/cli/tools/trello/internal/registry"
)

var (
	relativePattern = regexp.MustCompile(`^-([0-9]+)([smhdw])$`)
	datePattern     = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	epochPattern    = regexp.MustCompile(`^([0-9]{10}|[0-9]{13})$`)
)

var relativeUnits = map[string]time.Duration{
	"s": time.Second, "m": time.Minute, "h": time.Hour, "d": 24 * time.Hour, "w": 7 * 24 * time.Hour,
}

// parseInstant accepts exactly the forms of contract §8.5; anything else is usage, because guessing
// a timezone gives answers that look right and are not.
func parseInstant(flag, text string, now time.Time) (time.Time, *errs.Error) {
	switch {
	case text == "now" && flag == "until":
		return now, nil
	case relativePattern.MatchString(text):
		match := relativePattern.FindStringSubmatch(text)
		count, _ := strconv.ParseInt(match[1], 10, 64)
		return now.Add(-time.Duration(count) * relativeUnits[match[2]]), nil
	case datePattern.MatchString(text):
		if parsed, err := time.Parse("2006-01-02", text); err == nil {
			return parsed.UTC(), nil
		}
	case epochPattern.MatchString(text):
		number, _ := strconv.ParseInt(text, 10, 64)
		if len(text) == 13 {
			return time.UnixMilli(number).UTC(), nil
		}
		return time.Unix(number, 0).UTC(), nil
	default:
		if parsed, err := time.Parse(time.RFC3339, text); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, errs.Usagef("--%s %q is not an accepted time.", flag, text).
		WithHint("Use -2h, -7d, 2026-09-29, 2026-09-29T14:00:00+02:00, epoch seconds, or now (for --until).").
		WithDetail("flag", "--"+flag)
}

// resolveWindow applies the defaults (--since -24h, --until now) and reports where they came from.
func resolveWindow(invocation *Invocation, now time.Time) (*registry.Window, string, *errs.Error) {
	window := &registry.Window{Since: now.Add(-24 * time.Hour), Until: now}
	source := "default"
	if text, ok := invocation.Flags["since"]; ok {
		since, err := parseInstant("since", text, now)
		if err != nil {
			return nil, "", err
		}
		window.Since, source = since, "flag"
	}
	if text, ok := invocation.Flags["until"]; ok {
		until, err := parseInstant("until", text, now)
		if err != nil {
			return nil, "", err
		}
		window.Until, source = until, "flag"
	}
	if !window.Since.Before(window.Until) {
		return nil, "", errs.Usagef("--since must be earlier than --until.")
	}
	return window, source, nil
}

func formatInstant(instant time.Time) string { return instant.UTC().Format(time.RFC3339) }
