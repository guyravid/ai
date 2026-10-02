package config

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
)

type Input struct {
	Tool        string            // "telegram"
	Prefix      string            // "TELEGRAM"
	Flags       map[string]string // flags given on argv, keyed by name without dashes
	Env         Env
	GOOS        string
	Credentials []string // declared credential names, such as API_KEY
}

type Setting struct {
	Def     *Def
	Value   any // int64 or string; nil when unset
	Source  string
	Origin  Origin
	Default any
}

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Resolved is the single structure list-config prints, teach config describes, and doctor checks.
type Resolved struct {
	Prefix        string
	Profile       string
	File          *File
	ConfigPath    string // the file in effect, whether or not it exists; "" when undeterminable
	Settings      []*Setting
	Warnings      []Warning
	DatasetDirErr error // set when no dataset directory could be determined
	byName        map[string]*Setting
}

func (r *Resolved) Get(name string) *Setting { return r.byName[name] }

func (r *Resolved) Int(name string) int64 {
	value, _ := r.byName[name].Value.(int64)
	return value
}

func (r *Resolved) String(name string) string {
	value, _ := r.byName[name].Value.(string)
	return value
}

func (r *Resolved) Duration(name string) time.Duration {
	duration, _ := ParseDuration(r.String(name))
	return duration
}

func (r *Resolved) Size(name string) int64 {
	size, _ := ParseSize(r.String(name))
	return size
}

// Load resolves every setting through the seven tiers of contract §13.2, per setting.
func Load(in Input) (*Resolved, *errs.Error) {
	defs := Defs()
	resolved := &Resolved{Prefix: in.Prefix, byName: map[string]*Setting{}}

	profile, profileSetting, err := resolveProfile(in)
	if err != nil {
		return nil, err
	}
	resolved.Profile = profile

	configSetting, required, cfgErr := resolveConfigPath(in, profile)
	if cfgErr != nil {
		return nil, cfgErr
	}
	if path, ok := configSetting.Value.(string); ok {
		resolved.ConfigPath = path
		file, loadErr := LoadFile(path, required, defs, in.Credentials)
		if loadErr != nil {
			return nil, loadErr
		}
		resolved.File = file
	}

	if profile != "" && !profileExists(in, resolved.File, profile, defs) {
		return nil, errs.Configf("Profile %q is not declared in the config file or by any %s_<SETTING>_%s variable.",
			profile, in.Prefix, ProfileSuffix(profile)).
			WithHint(fmt.Sprintf("Run `%s list-profiles` to see the profiles you can use.", in.Tool)).
			WithDetail("profile", profile)
	}

	for _, def := range defs {
		var setting *Setting
		switch def.Name {
		case "CONFIG":
			setting = configSetting
		case "PROFILE":
			setting = profileSetting
		default:
			var resolveErr *errs.Error
			setting, resolveErr = resolveSetting(in, resolved, def, profile)
			if resolveErr != nil {
				return nil, resolveErr
			}
		}
		setting.Def = def
		resolved.Settings = append(resolved.Settings, setting)
		resolved.byName[def.Name] = setting
	}
	return resolved, nil
}

func resolveProfile(in Input) (string, *Setting, *errs.Error) {
	setting := &Setting{Source: "builtin", Origin: OriginBuiltin}
	var badSource string
	if value, ok := in.Flags["profile"]; ok {
		setting.Value, setting.Source, setting.Origin = value, "--profile", OriginFlag
		badSource = "flag"
	} else if value, ok := lookup(in.Env, in.Prefix+"_PROFILE"); ok {
		setting.Value, setting.Source, setting.Origin = value, in.Prefix+"_PROFILE", OriginEnvDefault
		badSource = "env"
	} else if value, ok := lookup(in.Env, "AGENTCLI_PROFILE"); ok {
		setting.Value, setting.Source, setting.Origin = value, "AGENTCLI_PROFILE", OriginFamily
		badSource = "env"
	}
	profile, _ := setting.Value.(string)
	if profile == "" {
		return "", setting, nil
	}
	if err := ValidateProfileName(profile); err != nil {
		code := errs.Config
		if badSource == "flag" {
			code = errs.Usage
		}
		return "", nil, errs.New(code, "%s.", capitalize(err.Error())).WithDetail("source", setting.Source)
	}
	return profile, setting, nil
}

// resolveConfigPath returns the CONFIG setting and whether the file it names must exist.
func resolveConfigPath(in Input, profile string) (*Setting, bool, *errs.Error) {
	candidates := []struct {
		name   string
		origin Origin
		isFlag bool
	}{
		{"--config", OriginFlag, true},
		{in.Prefix + "_CONFIG_" + ProfileSuffix(profile), OriginEnvProfile, false},
		{in.Prefix + "_CONFIG", OriginEnvDefault, false},
		{"AGENTCLI_CONFIG", OriginFamily, false},
	}
	var defaultPath any
	base, baseErr := ConfigBase(in.Env, in.GOOS)
	if baseErr == nil {
		defaultPath = filepath.Join(base, "agentcli", in.Tool, "config.json")
	}
	for _, candidate := range candidates {
		var value string
		var ok bool
		if candidate.isFlag {
			value, ok = in.Flags["config"]
		} else if candidate.origin == OriginEnvProfile && profile == "" {
			continue
		} else {
			value, ok = lookup(in.Env, candidate.name)
		}
		if !ok {
			continue
		}
		expanded, err := ExpandHome(value, in.Env, in.GOOS)
		if err != nil {
			return nil, false, errs.Configf("Cannot expand %s: %s.", candidate.name, err.Error())
		}
		return &Setting{Value: expanded, Source: candidate.name, Origin: candidate.origin, Default: defaultPath}, true, nil
	}
	if baseErr != nil {
		return nil, false, errs.Configf("The config directory cannot be determined: %s.", baseErr.Error()).
			WithHint(fmt.Sprintf("Pass --config <path> and --dataset-dir <path>, or set %s_CONFIG and %s_DATASET_DIR.", in.Prefix, in.Prefix))
	}
	return &Setting{Value: defaultPath, Source: "builtin", Origin: OriginBuiltin, Default: defaultPath}, false, nil
}

func profileExists(in Input, file *File, profile string, defs []*Def) bool {
	if file != nil {
		if _, ok := file.Profiles[profile]; ok {
			return true
		}
	}
	suffix := "_" + ProfileSuffix(profile)
	names := make([]string, 0, len(defs)+2*len(in.Credentials))
	for _, def := range defs {
		names = append(names, def.Name)
	}
	for _, credential := range in.Credentials {
		names = append(names, credential, credential+"_FILE")
	}
	for _, name := range names {
		if _, ok := lookup(in.Env, in.Prefix+"_"+name+suffix); ok {
			return true
		}
	}
	return false
}

func resolveSetting(in Input, resolved *Resolved, def *Def, profile string) (*Setting, *errs.Error) {
	setting := &Setting{Default: def.Default}
	if def.Name == "DATASET_DIR" {
		if base, err := CacheBase(in.Env, in.GOOS); err == nil {
			setting.Default = filepath.Join(base, "agentcli", in.Tool, "datasets")
		} else {
			resolved.DatasetDirErr = err
		}
	}

	type tier struct {
		origin Origin
		source string
		text   string
		raw    json.RawMessage
		found  bool
	}
	var tiers []tier
	if def.Flag != "" {
		value, ok := in.Flags[strings.TrimPrefix(def.Flag, "--")]
		tiers = append(tiers, tier{OriginFlag, def.Flag, value, nil, ok})
	}
	if profile != "" {
		name := in.Prefix + "_" + def.Name + "_" + ProfileSuffix(profile)
		value, ok := lookup(in.Env, name)
		tiers = append(tiers, tier{OriginEnvProfile, name, value, nil, ok})
	}
	name := in.Prefix + "_" + def.Name
	value, ok := lookup(in.Env, name)
	tiers = append(tiers, tier{OriginEnvDefault, name, value, nil, ok})
	if def.InFile && resolved.File != nil {
		if profile != "" {
			raw, ok := resolved.File.Section(profile)[def.FileKey()]
			tiers = append(tiers, tier{OriginFileProfile, resolved.File.Pointer(profile, def.FileKey()), "", raw, ok})
		}
		raw, ok := resolved.File.Section("")[def.FileKey()]
		tiers = append(tiers, tier{OriginFileDefault, resolved.File.Pointer("", def.FileKey()), "", raw, ok})
	}
	familyName := "AGENTCLI_" + def.Name
	value, ok = lookup(in.Env, familyName)
	tiers = append(tiers, tier{OriginFamily, familyName, value, nil, ok})

	for _, candidate := range tiers {
		if !candidate.found {
			continue
		}
		var parsed any
		var err error
		if candidate.raw != nil {
			parsed, err = parseFileValue(def, candidate.raw)
		} else {
			parsed, err = parseValue(def, candidate.text)
		}
		if err != nil {
			code := errs.Config
			if candidate.origin == OriginFlag {
				code = errs.Usage
			}
			return nil, errs.New(code, "Invalid value for %s from %s: %s.", def.Name, candidate.source, err.Error()).
				WithDetail("setting", def.Name).WithDetail("source", candidate.source)
		}
		if def.Kind == KindPath {
			expanded, expandErr := ExpandHome(parsed.(string), in.Env, in.GOOS)
			if expandErr != nil {
				return nil, errs.Configf("Cannot expand %s from %s: %s.", def.Name, candidate.source, expandErr.Error())
			}
			parsed = expanded
			if def.Name == "DATASET_DIR" {
				resolved.DatasetDirErr = nil
			}
		}
		if number, isInt := parsed.(int64); isInt && def.Max > 0 && number > def.Max {
			resolved.Warnings = append(resolved.Warnings, Warning{
				Code:    "setting_clamped",
				Message: fmt.Sprintf("%s %d exceeds the maximum of %d and was reduced.", def.Name, number, def.Max),
			})
			parsed = def.Max
		}
		setting.Value, setting.Source, setting.Origin = parsed, candidate.source, candidate.origin
		return setting, nil
	}
	setting.Value, setting.Source, setting.Origin = setting.Default, "builtin", OriginBuiltin
	return setting, nil
}

func lookup(env Env, name string) (string, bool) {
	value, ok := env(name)
	if !ok || value == "" {
		return "", false
	}
	return value, true
}

func capitalize(text string) string {
	if text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:]
}
