package secrets

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
)

// FileCheck describes how a protected file's permissions were verified.
type FileCheck struct {
	Path string
	Mode string // "0600", "0400", or "unverified"
}

// CheckProtected verifies that a file holding a credential, token, or private key is owner-only.
// Where the platform has no POSIX modes, it reports "unverified" instead of failing.
func CheckProtected(path, goos string) (FileCheck, *errs.Error) {
	info, err := os.Stat(path)
	if err != nil {
		return FileCheck{}, errs.Configf("The credential file %s cannot be read.", path).
			WithHint("Check that the path exists and is readable by this user.").
			WithDetail("file", path)
	}
	if !info.Mode().IsRegular() {
		return FileCheck{}, errs.Configf("The credential file %s is not a regular file.", path).WithDetail("file", path)
	}
	if goos == "windows" {
		return FileCheck{Path: path, Mode: "unverified"}, nil
	}
	perm := info.Mode().Perm()
	if perm != 0o600 && perm != 0o400 {
		return FileCheck{}, errs.Configf("The credential file %s has mode %04o; it must be 0600 or 0400.", path, perm).
			WithHint(fmt.Sprintf("chmod 600 %s", shellQuote(path))).
			WithDetail("file", path).WithDetail("mode", fmt.Sprintf("%04o", perm))
	}
	return FileCheck{Path: path, Mode: fmt.Sprintf("%04o", perm)}, nil
}

// ReadProtected checks a file's permissions, then returns its trimmed contents.
func ReadProtected(path, goos string) (string, FileCheck, *errs.Error) {
	check, err := CheckProtected(path, goos)
	if err != nil {
		return "", check, err
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		return "", check, errs.Configf("The credential file %s cannot be read.", path).WithDetail("file", path)
	}
	value := strings.TrimSpace(string(content))
	if value == "" {
		return "", check, errs.Configf("The credential file %s is empty.", path).WithDetail("file", path)
	}
	return value, check, nil
}

// parseEnvFile returns only the wanted keys from KEY=value lines. Every other line is discarded
// inside this function, so undeclared values never reach the rest of the tool.
func parseEnvFile(content []byte, wanted map[string]bool) map[string]string {
	found := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if !wanted[key] {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		if value != "" {
			found[key] = value
		}
	}
	return found
}

func shellQuote(path string) string {
	if strings.ContainsAny(path, " '\"$`\\") {
		return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
	}
	return path
}
