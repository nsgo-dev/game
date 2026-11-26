package editor

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestParseUntrustedMCPGeneratorResultCapsBeforeDecode(t *testing.T) {
	// 1. Oversized base64 string containing invalid base64 chars MUST be rejected by length check BEFORE DecodeString.
	hugeInvalidB64 := strings.Repeat("!", base64.StdEncoding.EncodedLen(maxGeneratedImageBytes)+8)
	raw, _ := json.Marshal(map[string]any{
		"candidates": []map[string]any{{"mimeType": "image/png", "data": hugeInvalidB64}},
	})
	_, err := parseUntrustedMCPGeneratorResult(&sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(raw)}},
	}, AssetGenImages)
	if err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("oversized base64 err = %v, want pre-decode exceeds limit error", err)
	}

	// 2. Total decoded payload across ImageContent blocks exceeding 64 MiB is rejected.
	chunk := make([]byte, maxGeneratedImageBytes)
	blocks := make([]sdkmcp.Content, 5) // 5 * 16 MiB = 80 MiB > 64 MiB
	for i := range blocks {
		blocks[i] = &sdkmcp.ImageContent{MIMEType: "image/png", Data: chunk}
	}
	_, err = parseUntrustedMCPGeneratorResult(&sdkmcp.CallToolResult{Content: blocks}, AssetGenImages)
	if err == nil || !strings.Contains(err.Error(), "total payload") {
		t.Fatalf("total payload cap err = %v, want total payload error", err)
	}

	// 3. NaN / Inf / negative ElapsedSeconds never panic or overflow.
	for _, badElapsed := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -42} {
		res, err := parseUntrustedMCPGeneratorResult(&sdkmcp.CallToolResult{
			StructuredContent: map[string]any{
				"elapsedSeconds": badElapsed,
			},
			Content: []sdkmcp.Content{
				&sdkmcp.ImageContent{MIMEType: "image/png", Data: []byte{1, 2, 3, 4}},
			},
		}, AssetGenImages)
		if err != nil {
			t.Fatalf("elapsed=%v unexpected err: %v", badElapsed, err)
		}
		if res.Elapsed != 0 {
			t.Fatalf("elapsed=%v produced Duration=%v, want 0", badElapsed, res.Elapsed)
		}
	}
}
