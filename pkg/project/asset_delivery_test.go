package project

import "testing"

func TestAssetDeliveryRejectsDuplicateGroupsAndNegativeCaps(t *testing.T) {
	if err := validateAssetDelivery("delivery", &AssetDelivery{Main: []string{"ui"}, Remote: []string{"ui"}}); err == nil {
		t.Fatal("duplicate group accepted")
	}
	if err := validateAssetDelivery("delivery", &AssetDelivery{Caps: &AssetCaps{Main: -1}}); err == nil {
		t.Fatal("negative cap accepted")
	}
}
