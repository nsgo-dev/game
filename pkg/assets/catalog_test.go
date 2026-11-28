package assets_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/nsgo-dev/game/pkg/assets"
)

func TestSignedCatalogCoversPackMetadata(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	catalog, err := assets.SignCatalog(priv, assets.Catalog{Serial: 2, Packs: []assets.PackSpec{{ID: "chapter", Version: "1.0.0", URL: "https://example.test/a.zip", SHA256: "0000000000000000000000000000000000000000000000000000000000000000"}}})
	if err != nil || assets.VerifyCatalog(pub, catalog) != nil {
		t.Fatalf("signed catalog: %v", err)
	}
	catalog.Packs[0].Version = "2.0.0"
	if !errors.Is(assets.VerifyCatalog(pub, catalog), assets.ErrInvalidCatalogSignature) {
		t.Fatal("tampered catalog accepted")
	}
}
