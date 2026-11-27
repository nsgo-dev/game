package wire

import "errors"

var (
	// ErrShortBuffer is returned when the input slice ends before a message or field is complete.
	ErrShortBuffer = errors.New("wire: short buffer")
	// ErrOverflow is returned when a varint overflows 64 bits or exceeds the field's declared bit width.
	ErrOverflow = errors.New("wire: integer overflow")
	// ErrInvalidBool is returned when a boolean byte on the wire is neither 0 nor 1.
	ErrInvalidBool = errors.New("wire: invalid boolean value")
	// ErrInvalidEnum is returned when a decoded enum integer does not match any declared enum value.
	ErrInvalidEnum = errors.New("wire: unknown enum value")
	// ErrInvalidTag is returned when a union discriminator does not match any declared case tag.
	ErrInvalidTag = errors.New("wire: unknown message tag")
	// ErrInvalidVersion is returned when a nibble-split discriminator carries an unexpected high-bit version value.
	ErrInvalidVersion = errors.New("wire: unexpected protocol version")
	// ErrInvalidConst is returned when a constant wire field does not match its declared value.
	ErrInvalidConst = errors.New("wire: unexpected constant field value")
	// ErrInvalidMask is returned when a presence bitmask sets bits beyond the mask group's declared fields.
	ErrInvalidMask = errors.New("wire: unknown presence mask bit")
	// ErrNonZeroPadding is returned when trailing padding bits in a bit-packed byte are non-zero.
	ErrNonZeroPadding = errors.New("wire: non-zero bit padding")
	// ErrLimitExceeded is returned when a variable-length bytes, string, or array field exceeds its schema maxLength.
	ErrLimitExceeded = errors.New("wire: length limit exceeded")
)
