package secrets

import (
	"fmt"
	"os"
	"strings"

	"github.com/guyravid/ai/cli/tools/trello/internal/config"
	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
)

// Credential is one resolved credential. Only Secret holds the value.
type Credential struct {
	Name   string
	Secret Secret
	Set    bool
	Source string
	Origin config.Origin
	File   *FileCheck // the file the value was read from, if any
}

type Input struct {
	Prefix  string
	Profile string
	Env     config.Env
	GOOS    string
	File    *config.File
	Names   []string
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
	type envScope struct {
		suffix string
		origin config.Origin
	}
	var scopes []envScope
	if in.Profile != "" {
		scopes = append(scopes, envScope{"_" + config.ProfileSuffix(in.Profile), config.OriginEnvProfile})
	}
	scopes = append(scopes, envScope{"", config.OriginEnvDefault})

	for _, scope := range scopes {
		direct := in.Prefix + "_" + name + scope.suffix
		if value, ok := in.Env(direct); ok && value != "" {
			return &Credential{Name: name, Secret: NewSecret(value), Set: true, Source: direct, Origin: scope.origin}, nil
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
			return &Credential{Name: name, Secret: NewSecret(value), Set: true, Source: fileVar, Origin: scope.origin, File: &check}, nil
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
		key := strings.ToLower(name) + "_file"
		path, err := in.File.PathMember(scope.profile, key)
		if err != nil {
			return nil, errs.Configf("%s.", err.Error())
		}
		if path != "" {
			credential, readErr := fromFile(in, name, path, in.File.Pointer(scope.profile, key), scope.origin)
			if readErr != nil {
				return nil, readErr
			}
			return credential, nil
		}
		envFile, err := in.File.PathMember(scope.profile, "env_file")
		if err != nil {
			return nil, errs.Configf("%s.", err.Error())
		}
		if envFile != "" {
			credential, readErr := fromEnvFile(in, name, envFile, in.File.Pointer(scope.profile, "env_file"), scope.origin)
			if readErr != nil {
				return nil, readErr
			}
			if credential != nil {
				return credential, nil
			}
		}
	}
	return &Credential{Name: name, Source: "builtin", Origin: config.OriginBuiltin}, nil
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

// fromEnvFile returns nil when the env_file exists but does not hold this credential, so resolution
// continues to the next scope.
func fromEnvFile(in Input, name, path, pointer string, origin config.Origin) (*Credential, *errs.Error) {
	expanded, err := config.ExpandHome(path, in.Env, in.GOOS)
	if err != nil {
		return nil, errs.Configf("Cannot expand %s: %s.", pointer, err.Error())
	}
	check, checkErr := CheckProtected(expanded, in.GOOS)
	if checkErr != nil {
		return nil, checkErr.WithDetail("source", pointer)
	}
	content, readErr := os.ReadFile(expanded)
	if readErr != nil {
		return nil, errs.Configf("The env_file %s cannot be read.", expanded).WithDetail("source", pointer)
	}
	var keys []string
	if in.Profile != "" {
		keys = append(keys, in.Prefix+"_"+name+"_"+config.ProfileSuffix(in.Profile))
	}
	keys = append(keys, in.Prefix+"_"+name)
	wanted := map[string]bool{}
	for _, key := range keys {
		wanted[key] = true
	}
	found := parseEnvFile(content, wanted)
	for _, key := range keys {
		if value, ok := found[key]; ok {
			return &Credential{Name: name, Secret: NewSecret(value), Set: true, Source: pointer, Origin: origin, File: &check}, nil
		}
	}
	return nil, nil
}

// MissingHint names every way to supply a credential (contract §12.1).
func MissingHint(prefix, name string) string {
	lower := strings.ToLower(name)
	return fmt.Sprintf("Set %s_%s, or %s_%s_FILE=<path to a file holding it>, or %s_file=<path> in the config file",
		prefix, name, prefix, name, lower)
}
