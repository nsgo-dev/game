package assets

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"testing"
)

// patchedZip returns a valid one-entry ZIP whose central-directory entry claims
// a huge uncompressed size, as a hostile pack could.
func patchedZip(t *testing.T, claimed uint64) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("asset.txt")
	if err != nil {
		t.Fatalf("zip.Create: %v", err)
	}
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}
	data := buf.Bytes()
	i := bytes.Index(data, []byte("PK\x01\x02"))
	if i < 0 {
		t.Fatal("central directory not found")
	}
	// Central directory header: compressed size at +20, uncompressed at +24 (32-bit).
	binary.LittleEndian.PutUint32(data[i+20:], uint32(claimed))
	binary.LittleEndian.PutUint32(data[i+24:], uint32(claimed))
	return data
}

func TestValidatePackZipRefusesBombSizedEntries(t *testing.T) {
	data := patchedZip(t, 1<<29)
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	if err := validatePackZip(zr); err == nil {
		t.Fatal("validatePackZip accepted an entry claiming 1 TiB")
	}
}
