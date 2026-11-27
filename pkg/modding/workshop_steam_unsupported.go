//go:build ios || android || (!darwin && !linux && !windows)

package modding

import "context"

// SteamWorkshop is a stub on platforms where Steamworks is unavailable (web, iOS, Android).
type SteamWorkshop struct{}

// NewSteamWorkshop always returns [ErrWorkshopUnavailable] on platforms without Steamworks.
func NewSteamWorkshop(SteamWorkshopOptions) (*SteamWorkshop, error) {
	return nil, ErrWorkshopUnavailable
}

// Close is a no-op on unsupported platforms.
func (*SteamWorkshop) Close() error { return nil }

// CreateItem returns [ErrWorkshopUnavailable] on unsupported platforms.
func (*SteamWorkshop) CreateItem(context.Context, uint32) (PublishedFileID, error) {
	return 0, ErrWorkshopUnavailable
}

// SubmitUpdate returns [ErrWorkshopUnavailable] on unsupported platforms.
func (*SteamWorkshop) SubmitUpdate(context.Context, PublishRequest) (PublishResult, error) {
	return PublishResult{}, ErrWorkshopUnavailable
}

// Subscribe returns [ErrWorkshopUnavailable] on unsupported platforms.
func (*SteamWorkshop) Subscribe(context.Context, PublishedFileID) error {
	return ErrWorkshopUnavailable
}

// Unsubscribe returns [ErrWorkshopUnavailable] on unsupported platforms.
func (*SteamWorkshop) Unsubscribe(context.Context, PublishedFileID) error {
	return ErrWorkshopUnavailable
}

// SubscribedItems returns [ErrWorkshopUnavailable] on unsupported platforms.
func (*SteamWorkshop) SubscribedItems(context.Context) ([]PublishedFileID, error) {
	return nil, ErrWorkshopUnavailable
}

// ItemInstallInfo returns [ErrWorkshopUnavailable] on unsupported platforms.
func (*SteamWorkshop) ItemInstallInfo(context.Context, PublishedFileID) (InstallInfo, error) {
	return InstallInfo{}, ErrWorkshopUnavailable
}

// DownloadItem returns [ErrWorkshopUnavailable] on unsupported platforms.
func (*SteamWorkshop) DownloadItem(context.Context, PublishedFileID, bool) error {
	return ErrWorkshopUnavailable
}

// QueryItems returns [ErrWorkshopUnavailable] on unsupported platforms.
func (*SteamWorkshop) QueryItems(context.Context, WorkshopQuery) ([]WorkshopItem, error) {
	return nil, ErrWorkshopUnavailable
}
