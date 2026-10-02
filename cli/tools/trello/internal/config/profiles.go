package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
)

// DefaultProfile names running without --profile in list-profiles (contract §7.3).
const DefaultProfile = "default"

// Where a profile can be declared (contract §13.3).
const (
	DeclaredInFile        = "config_file"
	DeclaredInEnvironment = "environment"
)

type ProfileEntry struct {
	Name       string   `json:"name"`
	Active     bool     `json:"active"`
	DeclaredIn []string `json:"declared_in"`
}

// ProfileList is what list-profiles reports: the entries, and the selected profile when it is not
// declared anywhere (which list-profiles reports as a warning rather than an error).
type ProfileList struct {
	Entries    []ProfileEntry
	Undeclared string
}

// ListProfiles lists the profiles the tool can run with (contract §7.3). environ is the whole
// environment as KEY=value pairs, since profiles declared by variables can only be found by
// enumerating them. The selected profile and config file resolve as for every other command.
func ListProfiles(in Input, environ []string) (*ProfileList, *errs.Error) {
	selected, _, err := resolveProfile(in)
	if err != nil {
		return nil, err
	}
	defs := Defs()
	configSetting, required, err := resolveConfigPath(in, selected)
	if err != nil {
		return nil, err
	}
	var file *File
	if path, ok := configSetting.Value.(string); ok {
		if file, err = LoadFile(path, required, defs, in.Credentials); err != nil {
			return nil, err
		}
	}

	bySuffix := map[string]*ProfileEntry{}
	add := func(name, declaredIn string) {
		suffix := ProfileSuffix(name)
		entry, ok := bySuffix[suffix]
		if !ok {
			entry = &ProfileEntry{Name: name, DeclaredIn: []string{}}
			bySuffix[suffix] = entry
		}
		for _, existing := range entry.DeclaredIn {
			if existing == declaredIn {
				return
			}
		}
		entry.DeclaredIn = append(entry.DeclaredIn, declaredIn)
	}
	if file != nil {
		for name := range file.Profiles {
			add(name, DeclaredInFile)
		}
	}
	for _, segment := range environmentProfiles(in, defs, environ) {
		add(strings.ToLower(segment), DeclaredInEnvironment)
	}

	list := &ProfileList{Entries: []ProfileEntry{}}
	if len(bySuffix) == 0 {
		if selected != "" {
			list.Undeclared = selected
		}
		return list, nil
	}
	declared := make([]ProfileEntry, 0, len(bySuffix))
	for _, entry := range bySuffix {
		sort.Strings(entry.DeclaredIn)
		entry.Active = selected != "" && ProfileSuffix(entry.Name) == ProfileSuffix(selected)
		declared = append(declared, *entry)
	}
	sort.Slice(declared, func(i, j int) bool { return declared[i].Name < declared[j].Name })
	list.Entries = append([]ProfileEntry{{Name: DefaultProfile, Active: selected == "", DeclaredIn: []string{}}}, declared...)
	if selected != "" && bySuffix[ProfileSuffix(selected)] == nil {
		list.Undeclared = selected
	}
	return list, nil
}

// environmentProfiles returns the profile segments of <PREFIX>_<SETTING>_<PROFILE> variables. Each
// variable is matched against the longest known setting or credential name, the same set that
// decides whether a selected profile exists, so the remainder is the profile (contract §13.3).
func environmentProfiles(in Input, defs []*Def, environ []string) []string {
	var names []string
	for _, def := range defs {
		names = append(names, def.Name)
	}
	for _, credential := range in.Credentials {
		names = append(names, credential, credential+"_FILE")
	}
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })

	prefix := in.Prefix + "_"
	var segments []string
	for _, pair := range environ {
		key, value, _ := strings.Cut(pair, "=")
		rest, ok := strings.CutPrefix(key, prefix)
		if !ok || value == "" {
			continue
		}
		for _, name := range names {
			segment, ok := strings.CutPrefix(rest, name+"_")
			if !ok {
				continue
			}
			if segment != "" && ValidateProfileName(segment) == nil {
				segments = append(segments, segment)
			}
			break
		}
	}
	return segments
}

// UndeclaredWarning is the warning list-profiles gives instead of failing.
func UndeclaredWarning(prefix, profile string) Warning {
	return Warning{Code: "profile_undeclared", Message: fmt.Sprintf(
		"Profile %q is selected but not declared in the config file or by any %s_<SETTING>_%s variable; other commands will fail with config.",
		profile, prefix, ProfileSuffix(profile))}
}
