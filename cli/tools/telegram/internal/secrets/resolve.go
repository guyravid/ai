package secrets

import (
	"fmt"
	"os"
	"strings"

	"github.com/guyravid/ai/cli/tools/telegram/internal/config"
	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
)

// Credential is one resolved credential. Only Secret holds the value.
type Credential struct {
	Name   string
	Secret Secret
	Set    bool
	Source string
	Origin config.Origin
	File   *FileCheck // the file the value was read from, if any
	// EnvFileKey is the KEY of the env_file line that supplied the value, when one did.
	EnvFileKey string
	// Skipped are the default-scope sources a selected profile did not use (Input.ScopedOnly), in the
	// order contract §12.1 would have tried them. Their values, where the tool had already read
	// them, are kept only so the redactor covers them.
	Skipped []Skipped
	// WouldUse names the default-scope source that would have supplied the credential if the profile
	// could use it. It is set only when nothing scoped to the profile resolved.
	WouldUse string
}

// Skipped is a default-scope credential source that was not used for a selected profile. Value is zero
// when the source is a file that was deliberately not opened.
type Skipped struct {
	Source string
	Value  Secret
}

type Input struct {
	Prefix  string
	Profile string
	Env     config.Env
	GOOS    string
	File    *config.File
	Names   []string
	// ScopedOnly narrows §12.1 for a selected profile: only the profile's own sources resolve, and
	// the default-scope ones (the unsuffixed variables, the default section's file, the default
	// env_file's unsuffixed key) are skipped. With no profile nothing is narrowed.
	ScopedOnly bool
}

// Resolve applies contract §12.1 to each declared credential. A credential that is simply absent is
// not an error here; handlers raise auth when they need it. A source that is named but unusable is
// config, and resolution never falls through past it.
func Resolve(in Input) ([]*Credential, []config.Warning, *errs.Error) {
	var credentials []*Credential
	var warnings []config.Warning
	for _, name := range in.Names {
		credential, err := resolveOne(in, name)
		if err != nil {
			return nil, nil, err
		}
		if credential.File != nil && credential.File.Mode == "unverified" {
			warnings = append(warnings, config.Warning{
				Code:    "permissions_unverified",
				Message: fmt.Sprintf("Permissions of %s could not be checked on this platform.", credential.File.Path),
			})
		}
		credentials = append(credentials, credential)
	}
	return credentials, warnings, nil
}

func resolveOne(in Input, name string) (*Credential, *errs.Error) {
	scoped := in.Profile != "" && in.ScopedOnly
	var skipped []Skipped
	if scoped {
		skipped = defaultScopeEnvironment(in, name)
	}
	found := func(credential *Credential) (*Credential, *errs.Error) {
		credential.Skipped = skipped
		return credential, nil
	}

	type envScope struct {
		suffix string
		origin config.Origin
	}
	var scopes []envScope
	if in.Profile != "" {
		scopes = append(scopes, envScope{"_" + config.ProfileSuffix(in.Profile), config.OriginEnvProfile})
	}
	if !scoped {
		scopes = append(scopes, envScope{"", config.OriginEnvDefault})
	}

	for _, scope := range scopes {
		direct := in.Prefix + "_" + name + scope.suffix
		if value, ok := in.Env(direct); ok && value != "" {
			return found(&Credential{Name: name, Secret: NewSecret(value), Set: true, Source: direct, Origin: scope.origin})
		}
		fileVar := in.Prefix + "_" + name + "_FILE" + scope.suffix
		if path, ok := in.Env(fileVar); ok && path != "" {
			expanded, err := config.ExpandHome(path, in.Env, in.GOOS)
			if err != nil {
				return nil, errs.Configf("Cannot expand %s: %s.", fileVar, err.Error())
			}
			value, check, readErr := ReadProtected(expanded, in.GOOS)
			if readErr != nil {
				return nil, readErr.WithDetail("source", fileVar)
			}
			return found(&Credential{Name: name, Secret: NewSecret(value), Set: true, Source: fileVar, Origin: scope.origin, File: &check})
		}
	}

	type fileScope struct {
		profile string
		origin  config.Origin
	}
	var fileScopes []fileScope
	if in.Profile != "" {
		fileScopes = append(fileScopes, fileScope{in.Profile, config.OriginFileProfile})
	}
	fileScopes = append(fileScopes, fileScope{"", config.OriginFileDefault})

	for _, scope := range fileScopes {
		if in.File == nil {
			break
		}
		inDefault := scope.profile == ""
		key := strings.ToLower(name) + "_file"
		path, err := in.File.PathMember(scope.profile, key)
		if err != nil {
			return nil, errs.Configf("%s.", err.Error())
		}
		if path != "" && scoped && inDefault {
			// Not opened: naming it is enough, and covering a value nobody read protects nothing.
			skipped = append(skipped, Skipped{Source: in.File.Pointer("", key)})
		} else if path != "" {
			credential, readErr := fromFile(in, name, path, in.File.Pointer(scope.profile, key), scope.origin)
			if readErr != nil {
				return nil, readErr
			}
			return found(credential)
		}
		envFile, err := in.File.PathMember(scope.profile, "env_file")
		if err != nil {
			return nil, errs.Configf("%s.", err.Error())
		}
		if envFile == "" {
			continue
		}
		credential, skip, readErr := fromEnvFile(in, name, envFile, in.File.Pointer(scope.profile, "env_file"), scope.origin, scoped && inDefault)
		if readErr != nil {
			return nil, readErr
		}
		if credential != nil {
			return found(credential)
		}
		if skip != nil {
			skipped = append(skipped, *skip)
		}
	}
	missing := &Credential{Name: name, Source: "builtin", Origin: config.OriginBuiltin, Skipped: skipped}
	if len(skipped) > 0 {
		missing.WouldUse = skipped[0].Source
	}
	return missing, nil
}

// defaultScopeEnvironment lists the unsuffixed environment sources a selected profile skips. Only the
// environment is read; a file named by TELEGRAM_BOT_TOKEN_FILE is not opened.
func defaultScopeEnvironment(in Input, name string) []Skipped {
	var skipped []Skipped
	direct := in.Prefix + "_" + name
	if value, ok := in.Env(direct); ok && value != "" {
		skipped = append(skipped, Skipped{Source: direct, Value: NewSecret(value)})
	}
	fileVar := in.Prefix + "_" + name + "_FILE"
	if path, ok := in.Env(fileVar); ok && path != "" {
		skipped = append(skipped, Skipped{Source: fileVar})
	}
	return skipped
}

func fromFile(in Input, name, path, pointer string, origin config.Origin) (*Credential, *errs.Error) {
	expanded, err := config.ExpandHome(path, in.Env, in.GOOS)
	if err != nil {
		return nil, errs.Configf("Cannot expand %s: %s.", pointer, err.Error())
	}
	value, check, readErr := ReadProtected(expanded, in.GOOS)
	if readErr != nil {
		return nil, readErr.WithDetail("source", pointer)
	}
	return &Credential{Name: name, Secret: NewSecret(value), Set: true, Source: pointer, Origin: origin, File: &check}, nil
}

// fromEnvFile returns nil when the env_file exists but does not hold a usable key, so resolution
// continues to the next scope. With defaultSkipped, the file is the default section's and the
// profile's own key is the only usable one: the unsuffixed key is reported as skipped instead.
func fromEnvFile(in Input, name, path, pointer string, origin config.Origin, defaultSkipped bool) (*Credential, *Skipped, *errs.Error) {
	expanded, err := config.ExpandHome(path, in.Env, in.GOOS)
	if err != nil {
		return nil, nil, errs.Configf("Cannot expand %s: %s.", pointer, err.Error())
	}
	check, checkErr := CheckProtected(expanded, in.GOOS)
	if checkErr != nil {
		return nil, nil, checkErr.WithDetail("source", pointer)
	}
	content, readErr := os.ReadFile(expanded)
	if readErr != nil {
		return nil, nil, errs.Configf("The env_file %s cannot be read.", expanded).WithDetail("source", pointer)
	}
	unsuffixed := in.Prefix + "_" + name
	var keys []string
	if in.Profile != "" {
		keys = append(keys, unsuffixed+"_"+config.ProfileSuffix(in.Profile))
	}
	if !defaultSkipped {
		keys = append(keys, unsuffixed)
	}
	wanted := map[string]bool{unsuffixed: true}
	for _, key := range keys {
		wanted[key] = true
	}
	values := parseEnvFile(content, wanted)
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return &Credential{Name: name, Secret: NewSecret(value), Set: true, Source: pointer, Origin: origin, File: &check, EnvFileKey: key}, nil, nil
		}
	}
	if value, ok := values[unsuffixed]; ok && defaultSkipped {
		return nil, &Skipped{Source: pointer, Value: NewSecret(value)}, nil
	}
	return nil, nil, nil
}

// MissingHint names every way to supply a credential (contract §12.1).
func MissingHint(prefix, name string) string {
	lower := strings.ToLower(name)
	return fmt.Sprintf("Set %s_%s, or %s_%s_FILE=<path to a file holding it>, or %s_file=<path> in the config file",
		prefix, name, prefix, name, lower)
}
