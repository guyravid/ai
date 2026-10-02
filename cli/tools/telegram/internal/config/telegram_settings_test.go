package config

import (
	"testing"
	"time"

	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
)

func TestChatSettingsDefaults(t *testing.T) {
	resolved := mustLoad(t, map[string]string{}, map[string]string{})
	if resolved.String("BASE_URL") != "https://api.telegram.org" {
		t.Errorf("BASE_URL = %q", resolved.String("BASE_URL"))
	}
	if resolved.String("DEFAULT_CHAT") != "" || resolved.String("ALLOWED_CHATS") != "" {
		t.Errorf("chat settings should default to empty")
	}
	if resolved.String("PARSE_MODE") != "none" {
		t.Errorf("PARSE_MODE = %q", resolved.String("PARSE_MODE"))
	}
	if resolved.Duration("POLL_INTERVAL") != 2*time.Second {
		t.Errorf("POLL_INTERVAL = %s", resolved.Duration("POLL_INTERVAL"))
	}
}

func TestValidChatID(t *testing.T) {
	for _, valid := range []string{"111111111", "-1001234567890", "0", "@news_channel", "99999999999999999999"} {
		if !ValidChatID(valid) {
			t.Errorf("%q should be valid", valid)
		}
	}
	for _, invalid := range []string{"", "-", "12a", "@abc", "@has space", "@" + "x123456789012345678901234567890123", "123456789012345678901", "--5", " 5", "5\n"} {
		if ValidChatID(invalid) {
			t.Errorf("%q should be invalid", invalid)
		}
	}
}

func TestChatSettingsAreValidatedAtLoad(t *testing.T) {
	cases := map[string]string{
		"TELEGRAM_DEFAULT_CHAT":  "not-a-chat",
		"TELEGRAM_ALLOWED_CHATS": "111,abc",
		"TELEGRAM_PARSE_MODE":    "markdown",
		"TELEGRAM_POLL_INTERVAL": "10ms",
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := load(t, map[string]string{}, map[string]string{name: value})
			if err == nil || err.Code != errs.Config {
				t.Fatalf("got %v, want config", err)
			}
		})
	}
}

func TestChatSettingsNormalise(t *testing.T) {
	resolved := mustLoad(t, map[string]string{}, map[string]string{
		"TELEGRAM_ALLOWED_CHATS": " 111 , -1001234567890,@news_channel ",
		"TELEGRAM_PARSE_MODE":    "HTML",
	})
	if got := resolved.String("ALLOWED_CHATS"); got != "111,-1001234567890,@news_channel" {
		t.Errorf("ALLOWED_CHATS = %q", got)
	}
	if got := resolved.String("PARSE_MODE"); got != "html" {
		t.Errorf("PARSE_MODE = %q", got)
	}
}

func TestProfileInheritsChatSettingsPerSetting(t *testing.T) {
	path := writeConfig(t, `{"config_version":1,
		"default":{"default_chat":"111","allowed_chats":"111,222","parse_mode":"html"},
		"profiles":{"family":{"default_chat":"-1001234567890","bot_token_file":"/k"},"builds":{"bot_token_file":"/k"}}}`)
	family := mustLoad(t, map[string]string{"config": path, "profile": "family"}, map[string]string{})
	if family.String("DEFAULT_CHAT") != "-1001234567890" || family.Get("DEFAULT_CHAT").Origin != OriginFileProfile {
		t.Errorf("family DEFAULT_CHAT = %q (%s)", family.String("DEFAULT_CHAT"), family.Get("DEFAULT_CHAT").Origin)
	}
	if family.String("ALLOWED_CHATS") != "111,222" || family.Get("ALLOWED_CHATS").Origin != OriginFileDefault {
		t.Errorf("family should inherit ALLOWED_CHATS from default, got %q (%s)", family.String("ALLOWED_CHATS"), family.Get("ALLOWED_CHATS").Origin)
	}
	builds := mustLoad(t, map[string]string{"config": path, "profile": "builds"}, map[string]string{})
	if builds.String("DEFAULT_CHAT") != "111" || builds.String("PARSE_MODE") != "html" {
		t.Errorf("builds should inherit default_chat and parse_mode from default")
	}
}

func TestChatSettingsInConfigFileMustBeValidStrings(t *testing.T) {
	for _, member := range []string{`"default_chat":"nope"`, `"default_chat":111`, `"parse_mode":"rich"`, `"poll_interval":2`, `"allowed_chats":"1,,2"`} {
		path := writeConfig(t, `{"config_version":1,"default":{`+member+`}}`)
		if _, err := load(t, map[string]string{"config": path}, map[string]string{}); err == nil || err.Code != errs.Config {
			t.Errorf("%s: got %v, want config", member, err)
		}
	}
}
