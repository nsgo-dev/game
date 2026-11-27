package accounts

import (
	"fmt"
	"io"
	"log/slog"
)

// redacted is how a Secret prints, logs and marshals.
const redacted = "[redacted]"

// Secret holds a credential (a session secret or a token) so that it cannot reach a log by accident: fmt, log/slog, encoding/json and encoding.TextMarshaler users see "[redacted]". Reveal returns the value for the one place that must send it.
type Secret struct{ s string }

// NewSecret wraps s.
func NewSecret(s string) Secret { return Secret{s} }

// Reveal returns the secret's value.
func (s Secret) Reveal() string { return s.s }

// IsZero reports whether the secret is empty.
func (s Secret) IsZero() bool { return s.s == "" }

// String returns "[redacted]".
func (s Secret) String() string { return redacted }

// GoString returns "[redacted]", for %#v.
func (s Secret) GoString() string { return redacted }

// Format writes "[redacted]" for every verb, so %x or %q cannot reveal the value either.
func (s Secret) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, redacted) }

// LogValue returns "[redacted]" for log/slog.
func (s Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalText returns "[redacted]".
func (s Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// MarshalJSON returns the JSON string "[redacted]".
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }
