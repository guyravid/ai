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

	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
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

// File is a parsed, validated config file. Credential entries are kept as paths only. Every section
// is keyed by flat member names, whatever nesting the file used (contract §13.4 rule 7), so a
// setting merges across sections one flat key at a time.
type File struct {
	Path     string
	Default  map[string]json.RawMessage
	Profiles map[string]map[string]json.RawMessage
	layout   *Layout
}

// Pointer names a member of the file for provenance, by its flat key, such as
// "config.json#profiles.work.limit". A nested member is named at its path:
// "config.json#profiles.work.telemetry.otlp.endpoint".
func (f *File) Pointer(profile, key string) string {
	return filepath.Base(f.Path) + "#" + sectionName(profile) + "." + f.layout.Path(key)
}

func sectionName(profile string) string {
	if profile == "" {
		return "default"
	}
	return "profiles." + profile
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
	text, err := pathString(raw)
	if err != nil {
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

// LoadFile reads and validates a config file whose layout has no nested credentials. It returns
// (nil, nil) when the file does not exist and was not required.
func LoadFile(path string, required bool, defs []*Def, credentials []string) (*File, *errs.Error) {
	layout, err := NewLayout(defs, credentials, nil)
	if err != nil {
		return nil, layoutError(err)
	}
	return LoadFileWithLayout(path, required, layout)
}

// LoadFileWithLayout reads and validates a config file against a layout. It returns (nil, nil) when
// the file does not exist and was not required.
func LoadFileWithLayout(path string, required bool, layout *Layout) (*File, *errs.Error) {
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

	file := &File{Path: path, layout: layout}
	if parsed.Default != nil {
		file.Default = map[string]json.RawMessage{}
		if err := file.flatten("", nil, layout.root, parsed.Default, file.Default); err != nil {
			return nil, err
		}
	}
	names := make([]string, 0, len(parsed.Profiles))
	for name := range parsed.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	if parsed.Profiles != nil {
		file.Profiles = map[string]map[string]json.RawMessage{}
	}
	for _, name := range names {
		if err := ValidateProfileName(name); err != nil {
			return nil, fileError(path, "profiles."+name, "The config file declares an invalid profile name: %s.", err.Error())
		}
		flat := map[string]json.RawMessage{}
		if err := file.flatten(name, nil, layout.root, parsed.Profiles[name], flat); err != nil {
			return nil, err
		}
		file.Profiles[name] = flat
	}
	return file, nil
}

// flatten validates the members of one block (the section itself when prefix is empty) and copies
// each leaf into out under its flat key. Members are visited in sorted order so the first error
// reported is stable.
func (f *File) flatten(profile string, prefix []string, node *layoutNode, members map[string]json.RawMessage, out map[string]json.RawMessage) *errs.Error {
	keys := make([]string, 0, len(members))
	for key := range members {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		path := append(append([]string{}, prefix...), key)
		relative := strings.Join(path, ".")
		member := sectionName(profile) + "." + relative
		raw := members[key]
		child, known := node.children[key]
		if !known {
			hint := "Run `list-config --schema` for the members the file accepts."
			if spelling := f.layout.spellingOf(path); spelling != "" {
				hint = fmt.Sprintf("This tool spells %s as %s; each setting has one spelling. %s", relative, spelling, hint)
			}
			return fileError(f.Path, member, "The config file has an unrecognised member %s.", member).WithHint(hint)
		}
		if child.children != nil {
			trimmed := bytes.TrimSpace(raw)
			var block map[string]json.RawMessage
			if len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(trimmed, &block) != nil {
				return fileError(f.Path, member, "The config file member %s must be an object, a block of settings.", member).
					WithHint("Run `list-config --schema` for the members the file accepts.")
			}
			if err := f.flatten(profile, path, child, block, out); err != nil {
				return err
			}
			continue
		}
		switch child.kind {
		case memberCredentialValue:
			// Never echo the value: the member name alone says what is wrong.
			return fileError(f.Path, member,
				"The config file sets %s to a value; credentials may only be referenced by path.", relative).
				WithHint(fmt.Sprintf("Replace %s with %s_file=<path to a file holding it>, or use env_file.", relative, relative))
		case memberDescription:
			if err := validateDescription(raw); err != nil {
				return fileError(f.Path, member, "The config file member %s is invalid: %s.", member, err.Error()).
					WithHint(fmt.Sprintf("Set %s to one line of 1 to %d characters saying what the profile is for.",
						f.Pointer(profile, child.flatKey), MaxDescriptionLength))
			}
		case memberEnvFile, memberCredentialFile:
			if _, err := pathString(raw); err != nil {
				return fileError(f.Path, member, "%s must be a non-empty path string.", f.Pointer(profile, child.flatKey))
			}
		default:
			if _, err := parseFileValue(child.def, raw); err != nil {
				return fileError(f.Path, member, "The config file member %s is invalid: %s.", member, err.Error())
			}
		}
		out[child.flatKey] = raw
	}
	return nil
}

func pathString(raw json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil || text == "" {
		return "", errors.New("not a non-empty string")
	}
	return text, nil
}

func layoutError(err error) *errs.Error {
	return errs.New(errs.Internal, "The tool declares an invalid config file layout: %s.", err.Error())
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
