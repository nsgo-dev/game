package liveops

import (
	"context"
	"errors"
)

// ErrSlotNotFound is returned by CloudSync.Load when the requested save slot does not exist.
var ErrSlotNotFound = errors.New("liveops: cloud save slot not found")

// ErrCloudUnavailable is returned by a CloudSync implementation when cloud storage is signed out, offline or unsupported on the current target.
var ErrCloudUnavailable = errors.New("liveops: cloud save backend unavailable")

// CloudSync is the provider-agnostic cloud-save interface for NSGo games (#196).
//
// Built-in platform adapters (iCloud, Google Play Games, Steam Cloud) wait on platform cloud-save bindings; games can supply their own backend or an in-memory mock against this interface today.
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
