package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
)

var profileNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// ValidateProfileName enforces the contract's profile naming (§13.3).
func ValidateProfileName(name string) error {
	if !profileNamePattern.MatchString(name) {
		return fmt.Errorf("profile %q must start with a letter or digit and contain only letters, digits, '-' and '_'", name)
	}
	if strings.EqualFold(name, "file") {
		return fmt.Errorf("a profile cannot be named FILE")
	}
	if strings.EqualFold(name, DefaultProfile) {
		return fmt.Errorf("a profile cannot be named default, which list-profiles uses for running without one")
	}
	return nil
}

// ProfileSuffix turns a profile name into an environment variable segment.
func ProfileSuffix(profile string) string {
	return strings.ToUpper(strings.ReplaceAll(profile, "-", "_"))
}

// File is a parsed, validated config file. Credential entries are kept as paths only.
type File struct {
	Path     string
	Default  map[string]json.RawMessage
	Profiles map[string]map[string]json.RawMessage
}

// Pointer names a member of the file for provenance, such as "config.json#profiles.work.limit".
func (f *File) Pointer(profile, key string) string {
	section := "default"
	if profile != "" {
		section = "profiles." + profile
	}
	return filepath.Base(f.Path) + "#" + section + "." + key
}

// Section returns the members of one section: the named profile, or default when profile is "".
func (f *File) Section(profile string) map[string]json.RawMessage {
	if f == nil {
		return nil
	}
	if profile == "" {
		return f.Default
	}
	return f.Profiles[profile]
}

// PathMember returns a string member that holds a path, or "" when absent.
func (f *File) PathMember(profile, key string) (string, error) {
	raw, ok := f.Section(profile)[key]
	if !ok {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil || text == "" {
		return "", fmt.Errorf("%s must be a non-empty path string", f.Pointer(profile, key))
	}
	return text, nil
}

// DescriptionKey is the config file member that says what a section is for. It is not a setting
// (contract §13.4 rule 6): it is read only by list-profiles.
const DescriptionKey = "description"

// MaxDescriptionLength is the longest description, in characters (contract §13.4 rule 6).
const MaxDescriptionLength = 200

// Description returns the description of one section (the named profile, or default when profile
// is ""), or nil when it has none. LoadFile has already validated it.
func (f *File) Description(profile string) *string {
	if f == nil {
		return nil
	}
	raw, ok := f.Section(profile)[DescriptionKey]
	if !ok {
		return nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return nil
	}
	return &text
}

// validateDescription checks a description member: a string on one line, at most
// MaxDescriptionLength characters, and not empty after trimming.
func validateDescription(raw json.RawMessage) error {
	var text string
	if string(bytes.TrimSpace(raw)) == "null" || json.Unmarshal(raw, &text) != nil {
		return errors.New("it must be a string")
	}
	if strings.ContainsAny(text, "\r\n") {
		return errors.New("it must be a single line")
	}
	if utf8.RuneCountInString(text) > MaxDescriptionLength {
		return fmt.Errorf("it must be at most %d characters", MaxDescriptionLength)
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("it must not be empty")
	}
	return nil
}

type rawFile struct {
	ConfigVersion *int                                  `json:"config_version"`
	Default       map[string]json.RawMessage            `json:"default"`
	Profiles      map[string]map[string]json.RawMessage `json:"profiles"`
}

// LoadFile reads and validates a config file. It returns (nil, nil) when the file does not exist
// and was not required.
func LoadFile(path string, required bool, defs []*Def, credentials []string) (*File, *errs.Error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !required {
			return nil, nil
		}
		return nil, errs.Configf("The config file %s cannot be read.", path).
			WithHint("Check the path given by --config or the _CONFIG variable.").
			WithDetail("file", path)
	}

	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var parsed rawFile
	if err := decoder.Decode(&parsed); err != nil {
		return nil, fileError(path, "", "The config file %s is not valid: %s.", path, describeJSONError(err))
	}
	// config_version is optional; when present it must be a version this tool understands.
	if parsed.ConfigVersion != nil && *parsed.ConfigVersion != 1 {
		return nil, fileError(path, "config_version", "The config file %s declares config_version %d; this tool understands 1.", path, *parsed.ConfigVersion)
	}

	file := &File{Path: path, Default: parsed.Default, Profiles: parsed.Profiles}
	if err := validateSection(file, "", parsed.Default, defs, credentials); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(parsed.Profiles))
	for name := range parsed.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ValidateProfileName(name); err != nil {
			return nil, fileError(path, "profiles."+name, "The config file declares an invalid profile name: %s.", err.Error())
		}
		if err := validateSection(file, name, parsed.Profiles[name], defs, credentials); err != nil {
			return nil, err
		}
	}
	return file, nil
}

func validateSection(file *File, profile string, section map[string]json.RawMessage, defs []*Def, credentials []string) *errs.Error {
	keys := make([]string, 0, len(section))
	for key := range section {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	byKey := map[string]*Def{}
	for _, def := range defs {
		if def.InFile {
			byKey[def.FileKey()] = def
		}
	}
	for _, key := range keys {
		member := strings.TrimPrefix(file.Pointer(profile, key), filepath.Base(file.Path)+"#")
		if isCredentialValueKey(key, credentials) {
			// Never echo the value: the member name alone says what is wrong.
			return fileError(file.Path, member,
				"The config file sets %s to a value; credentials may only be referenced by path.", key).
				WithHint(fmt.Sprintf("Replace %s with %s_file=<path to a file holding it>, or use env_file.", key, key))
		}
		if key == DescriptionKey {
			if err := validateDescription(section[key]); err != nil {
				return fileError(file.Path, member, "The config file member %s is invalid: %s.", member, err.Error()).
					WithHint(fmt.Sprintf("Set %s to one line of 1 to %d characters saying what the profile is for.",
						file.Pointer(profile, key), MaxDescriptionLength))
			}
			continue
		}
		if key == "env_file" || isCredentialFileKey(key, credentials) {
			if _, err := file.PathMember(profile, key); err != nil {
				return fileError(file.Path, member, "%s.", err.Error())
			}
			continue
		}
		def, known := byKey[key]
		if !known {
			return fileError(file.Path, member, "The config file has an unrecognised member %s.", member).
				WithHint("Run `list-config --schema` for the members the file accepts.")
		}
		if _, err := parseFileValue(def, section[key]); err != nil {
			return fileError(file.Path, member, "The config file member %s is invalid: %s.", member, err.Error())
		}
	}
	return nil
}

func isCredentialValueKey(key string, credentials []string) bool {
	for _, name := range credentials {
		if key == strings.ToLower(name) {
			return true
		}
	}
	return false
}

func isCredentialFileKey(key string, credentials []string) bool {
	for _, name := range credentials {
		if key == strings.ToLower(name)+"_file" {
			return true
		}
	}
	return false
}

func fileError(path, member, format string, args ...any) *errs.Error {
	err := errs.Configf(format, args...).WithDetail("file", path)
	if member != "" {
		err.WithDetail("member", member)
	}
	return err
}

// describeJSONError reports where parsing failed without quoting file content, which could include
// a credential someone pasted in by mistake.
func describeJSONError(err error) string {
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return fmt.Sprintf("JSON syntax error at byte %d", syntax.Offset)
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return fmt.Sprintf("member %s has the wrong type", typeErr.Field)
	}
	message := err.Error()
	if strings.HasPrefix(message, "json: unknown field ") {
		return "unrecognised member " + strings.TrimPrefix(message, "json: unknown field ")
	}
	return "it cannot be parsed as JSON"
}
