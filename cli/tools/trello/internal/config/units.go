package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	durationPattern = regexp.MustCompile(`^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$`)
	dayPattern      = regexp.MustCompile(`^([0-9]+)d$`)
	sizePattern     = regexp.MustCompile(`^([0-9]+)(B|KB|MB|GB|TB|KiB|MiB|GiB|TiB)$`)
)

var sizeUnits = map[string]int64{
	"B":   1,
	"KB":  1000,
	"MB":  1000 * 1000,
	"GB":  1000 * 1000 * 1000,
	"TB":  1000 * 1000 * 1000 * 1000,
	"KiB": 1 << 10,
	"MiB": 1 << 20,
	"GiB": 1 << 30,
	"TiB": 1 << 40,
}

// ParseDuration accepts Go durations with units ("30s", "1h30m") and whole days ("7d").
// A bare number is rejected: the contract requires units (§13.5).
func ParseDuration(text string) (time.Duration, error) {
	if match := dayPattern.FindStringSubmatch(text); match != nil {
		days, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	if !durationPattern.MatchString(text) {
		return 0, fmt.Errorf("%q is not a duration with units, such as 30s, 5m, 4h, or 7d", text)
	}
	return time.ParseDuration(text)
}

// ParseSize accepts a whole number followed by a unit: B, KB, MB, GB, TB (decimal) or KiB, MiB,
// GiB, TiB (binary).
func ParseSize(text string) (int64, error) {
	match := sizePattern.FindStringSubmatch(strings.TrimSpace(text))
	if match == nil {
		return 0, fmt.Errorf("%q is not a size with units, such as 256MB or 2GiB", text)
	}
	count, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return 0, err
	}
	return count * sizeUnits[match[2]], nil
}
