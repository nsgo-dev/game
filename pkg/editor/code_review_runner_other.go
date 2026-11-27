//go:build ios || android || js || wasm

package editor

import (
	"context"
	"errors"
)

// BuildGoOverlay is unavailable on mobile/web platforms without a local Go toolchain.
func BuildGoOverlay(projectDir string, pending map[string]PendingGoOverlayFile) (string, func(), error) {
	return "", nil, errors.New("editor: Go build overlay is only supported on desktop platforms")
}

func runDefaultCodeReviewChecks(ctx context.Context, req CodeReviewCheckRequest, onProgress func(CodeReviewCheckProgress)) (CodeReviewCheckResult, error) {
	err := errors.New("editor: running nsgo check and nsgo test is only supported on desktop platforms")
	return CodeReviewCheckResult{
		Phase:      CodeReviewChecksFailed,
		CheckError: err.Error(),
	}, err
}

func openExternalSourceFileOS(absPath string) error {
	return errors.New("editor: opening external editors is only supported on desktop platforms")
}
