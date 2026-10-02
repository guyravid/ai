// Package secrets resolves credentials (contract §12.1), checks the files they come from (§12.4), and
// builds the redactor every output passes through (§12.3).
package secrets

// Secret holds a credential value. Every printable and serialisable form is a placeholder, so an
// accidental log or marshal cannot leak it; Reveal has one legitimate caller, the auth header.
type Secret struct{ value string }

func NewSecret(value string) Secret { return Secret{value: value} }

func (Secret) String() string               { return "[secret]" }
func (Secret) GoString() string             { return "[secret]" }
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"[secret]"`), nil }
func (Secret) MarshalText() ([]byte, error) { return []byte("[secret]"), nil }
func (s Secret) Reveal() string             { return s.value }
func (s Secret) IsZero() bool               { return s.value == "" }
