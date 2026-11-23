package cloudsave

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

var (
	// ErrNotFound reports a slot, or a version of one, that the account does not have. Service.Load returns it for a deleted slot too.
	ErrNotFound = errors.New("cloudsave: not found")
	// ErrChanged reports a conditional write or delete whose precondition no longer holds because the slot changed. Nothing was written.
	ErrChanged = errors.New("cloudsave: slot changed")
	// ErrSlotTooLarge reports a payload over Config.MaxSlotBytes. Nothing was written.
	ErrSlotTooLarge = errors.New("cloudsave: slot too large")
	// ErrTooManySlots reports a write that would give the account more than Config.MaxSlots live slots. Nothing was written.
	ErrTooManySlots = errors.New("cloudsave: too many slots")
	// ErrInvalidSlot reports a slot name ValidateSlot refuses.
	ErrInvalidSlot = errors.New("cloudsave: invalid slot name")
	// ErrInvalidAccount reports an empty account ID or one longer than MaxAccountBytes.
	ErrInvalidAccount = errors.New("cloudsave: invalid account id")
)

// MaxSlotNameBytes is the longest slot name, in bytes; it matches the limit of save.ValidateSlot.
const MaxSlotNameBytes = 200

// MaxAccountBytes is the longest account ID, in bytes, the service files slots under.
const MaxAccountBytes = 256

// ValidateSlot checks that slot is a name the service stores: 1 to MaxSlotNameBytes bytes of UTF-8 with no control characters and no '/'. Every name save.ValidateSlot accepts passes.
func ValidateSlot(slot string) error {
	if slot == "" || len(slot) > MaxSlotNameBytes || !utf8.ValidString(slot) {
		return fmt.Errorf("%w: %q", ErrInvalidSlot, slot)
	}
	for _, r := range slot {
		if r < 0x20 || r >= 0x7f && r < 0xa0 || r == '/' {
			return fmt.Errorf("%w: %q", ErrInvalidSlot, slot)
		}
	}
	return nil
}

func validateAccount(account string) error {
	if account == "" || len(account) > MaxAccountBytes {
		return fmt.Errorf("%w: %q", ErrInvalidAccount, account)
	}
	return nil
}

// Hash returns the hash a slot's payload is identified by in versions, ETags and If-Match preconditions: "sha256:" followed by the lowercase hex SHA-256 of data. It equals liveops.PayloadHash.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Version is one version of a slot.
type Version struct {
	// Slot is the slot name.
	Slot string
	// Number counts the slot's versions from 1; each write or delete adds one.
	Number int64
	// Deleted marks a version that deleted the slot. It has no data and an empty hash.
	Deleted bool
	// Hash is Hash(Data).
	Hash string
	// Size is len(Data).
	Size int64
	// Created is when the version was written, to the millisecond.
	Created time.Time
	// Data is the payload. Listings (Store.Versions and Store.Slots) leave it nil.
	Data []byte
}

// Match is the precondition of a conditional write or delete.
type Match struct {
	// Any skips the check: the write applies whatever the slot holds.
	Any bool
	// Hash, when Any is false, is the hash of the payload the slot must hold; empty means the slot must not exist (never written, or deleted).
	Hash string
}

// MatchAny is the precondition that always holds.
var MatchAny = Match{Any: true}

// MatchHash is the precondition that the slot holds the payload with this hash, or, for "", that it does not exist.
func MatchHash(hash string) Match { return Match{Hash: hash} }

// Holds reports whether m holds for a slot whose newest version is head; exists is false when the slot has no versions. A Store's Append decides its precondition with it.
func (m Match) Holds(head Version, exists bool) bool {
	if m.Any {
		return true
	}
	if !exists || head.Deleted {
		return m.Hash == ""
	}
	return m.Hash == head.Hash
}

// Write is a version Store.Append adds.
type Write struct {
	// Delete adds a version that deletes the slot; Data is then ignored.
	Delete bool
	// Data is the new payload.
	Data []byte
	// Match is the precondition on the slot's newest version.
	Match Match
	// Created is the version's time.
	Created time.Time
	// Keep is how many of the slot's newest versions to keep, at least 1.
	Keep int
	// MaxSlots is how many live slots the account may have; a write that makes a slot live beyond it fails with ErrTooManySlots. Deleted slots beyond MaxSlots are purged, oldest first, with their history.
	MaxSlots int
}

// Store keeps slot versions per account. Implementations must be safe for concurrent use, and Append must be atomic: of two concurrent appends whose preconditions name the same newest version, at most one succeeds.
type Store interface {
	// Head returns the newest version of slot with its data, or ErrNotFound when the slot has none.
	Head(ctx context.Context, account, slot string) (Version, error)
	// Version returns version number of slot with its data, or ErrNotFound when it is not kept.
	Version(ctx context.Context, account, slot string, number int64) (Version, error)
	// Versions lists the kept versions of slot, newest first and without data, or returns ErrNotFound when the slot has none.
	Versions(ctx context.Context, account, slot string) ([]Version, error)
	// Slots lists the newest version of each of the account's slots, deleted ones included, sorted by slot name and without data.
	Slots(ctx context.Context, account string) ([]Version, error)
	// Append adds a version to slot when w.Match holds for its newest version, returning the new version without data. It returns ErrChanged when the precondition fails, ErrNotFound for a delete of a slot that does not exist (never written or already deleted), and ErrTooManySlots when the write would exceed w.MaxSlots live slots; in each case nothing is written. It drops all but the w.Keep newest versions of slot.
	Append(ctx context.Context, account, slot string, w Write) (Version, error)
	// Purge removes slot and every version of it; a slot with no versions is not an error.
	Purge(ctx context.Context, account, slot string) error
}
