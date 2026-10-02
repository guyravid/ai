package secrets

import (
	"fmt"
	"strings"

	"github.com/guyravid/ai/cli/tools/telegram/internal/config"
	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
)

// CheckProfileIsolation reports the failure of a selected profile that has no credential of its own.
// This tool narrows contract §12.1 for a selected profile: Resolve (with Input.ScopedOnly) uses only
// the profile's own sources, in this order, and skips the default-scope ones entirely:
//
//  1. <PREFIX>_<NAME>_<PROFILE>
//  2. <PREFIX>_<NAME>_FILE_<PROFILE>
//  3. profiles.<p>.<name>_file, or profiles.<p>.env_file holding the key
//  4. a default.env_file line keyed <PREFIX>_<NAME>_<PROFILE>
//
// A profile therefore uses its own file even when a default-scope variable is set. When
// nothing scoped resolved but a default-scope source exists, this is a config error naming that source,
// because sending as the default bot would be the wrong bot. When nothing exists at all it returns
// nil: the caller reports the ordinary missing credential as auth.
func CheckProfileIsolation(credential *Credential, prefix, profile string) *errs.Error {
	if profile == "" || credential == nil || credential.Set || credential.WouldUse == "" {
		return nil
	}
	lower := strings.ToLower(credential.Name)
	return errs.Configf("Profile '%s' has no %s of its own and would use the default bot's.",
		profile, strings.ReplaceAll(lower, "_", " ")).
		WithHint(fmt.Sprintf("Set profiles.%s.%s_file in the config file, or %s_%s_FILE_%s",
			profile, lower, prefix, credential.Name, config.ProfileSuffix(profile))).
		WithDetail("profile", profile).
		WithDetail("would_use_source", credential.WouldUse)
}
