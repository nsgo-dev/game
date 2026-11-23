package liveops

import (
	"context"
	"errors"
)

// ErrSlotNotFound is returned by CloudSync.Load when the requested save slot does not exist.
var ErrSlotNotFound = errors.New("liveops: cloud save slot not found")

// ErrCloudUnavailable is returned by a CloudSync implementation when cloud storage is signed out, offline or unsupported on the current target.
var ErrCloudUnavailable = errors.New("liveops: cloud save backend unavailable")

// ErrSlotTooLarge is returned by a CloudSync implementation whose backend refuses a save payload over its size limit. The slot is left as it was.
var ErrSlotTooLarge = errors.New("liveops: cloud save slot too large")

// ErrTooManySlots is returned by a CloudSync implementation whose backend refuses a new slot because the player already has as many as it keeps. The slot is not created.
var ErrTooManySlots = errors.New("liveops: too many cloud save slots")

// CloudSync is the provider-agnostic cloud-save interface for NSGo games (#196).
//
// Built-in platform adapters (iCloud, Google Play Games, Steam Cloud) wait on platform cloud-save bindings; CloudClient talks to the self-hosted backend in package cloudsave, and games can supply their own backend or an in-memory mock against this interface.
type CloudSync interface {
	// Load reads the raw bytes stored in slot, returning ErrSlotNotFound if slot does not exist.
	Load(ctx context.Context, slot string) ([]byte, error)
	// Save writes data to slot, replacing any previous contents.
	Save(ctx context.Context, slot string, data []byte) error
	// Delete removes slot if it exists.
	Delete(ctx context.Context, slot string) error
	// List returns the names of all save slots stored for the current player.
	List(ctx context.Context) ([]string, error)
}

// ConditionalCloudSync is a CloudSync whose backend can write and delete a slot only while it still holds the payload the caller last read, so two devices writing the same slot at once cannot overwrite each other. CloudSyncer uses these methods whenever its Cloud implements them; CloudClient does.
//
// A payload is identified by PayloadHash of its raw bytes, as Load returned them.
type ConditionalCloudSync interface {
	CloudSync
	// SaveIf writes data to slot only while the slot holds the payload whose PayloadHash is match, or, when match is empty, only while the slot does not exist. Otherwise it writes nothing and returns an error wrapping ErrCloudChanged.
	SaveIf(ctx context.Context, slot string, data []byte, match string) error
	// DeleteIf removes slot only while it holds the payload whose PayloadHash is match. Otherwise it removes nothing and returns an error wrapping ErrCloudChanged. An empty match succeeds, removing nothing, while slot does not exist.
	DeleteIf(ctx context.Context, slot, match string) error
}

// PayloadHash identifies a raw cloud payload for ConditionalCloudSync: "sha256:" followed by the lowercase hex SHA-256 of payload.
func PayloadHash(payload []byte) string { return hashData(payload) }
