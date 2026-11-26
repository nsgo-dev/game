package editormcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/packages/game/editor"
)

const (
	// ToolReleaseNotesDraft drafts player-facing Markdown release notes from commits and kept reviews
	// and writes them to the exact file `game release -release-notes` reads.
	ToolReleaseNotesDraft = "release_notes_draft"
	// ToolReleaseReviewUploads stages the Review uploads confirmation dialog for the person.
	// Store uploads ALWAYS ask first in Review uploads, whatever the permission profile.
	ToolReleaseReviewUploads = "release_review_uploads"
)

func releaseTools() []Tool {
	return []Tool{
		{
			Name:        ToolReleaseNotesDraft,
			Title:       "Draft release notes",
			Description: "Draft player-facing Markdown release notes from commits since the last release tag (or initial commit) and kept reviews, saving them directly to the Markdown file `game release -release-notes` reads. Optionally accepts custom markdown or kept_reviews to include.",
			InputSchema: []byte(`{"type":"object","additionalProperties":false,"properties":{"markdown":{"type":"string","description":"Optional player-facing Markdown release notes to save directly to the release notes file; when omitted, drafts from git commits and kept reviews"},"kept_reviews":{"type":"array","items":{"type":"string"},"description":"Optional kept review summaries to include when drafting"}}}`),
			Action:      ActionEdit,
			Handler:     releaseNotesDraftTool,
		},
		{
			Name:        ToolReleaseReviewUploads,
			Title:       "Review release uploads",
			Description: "Open the Review uploads confirmation dialog listing every artifact, its file size, store destination (itch.io, Google Play internal testing, TestFlight), and version. Never starts an upload: store uploads always require the person's explicit confirmation in Review uploads, whatever the permission profile.",
			InputSchema: []byte(`{"type":"object","additionalProperties":false,"properties":{}}`),
			Action:      ActionRun,
			Handler:     releaseReviewUploadsTool,
		},
	}
}

func releaseNotesDraftTool(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		Markdown    string   `json:"markdown"`
		KeptReviews []string `json:"kept_reviews"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, err
	}
	var (
		notesFile  string
		provenance string
		markdown   string
	)
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		if ed.Project == nil {
			return editor.ErrNoProject
		}
		if len(args.KeptReviews) > 0 {
			ed.SetReleaseKeptReviews(args.KeptReviews)
		}
		if strings.TrimSpace(args.Markdown) != "" {
			ed.SetReleaseNotesText(args.Markdown)
			snap := ed.BuildReleaseSnapshot()
			notesFile = snap.ReleaseNotesFile
			provenance = snap.ReleaseNotesProvenance
			markdown = snap.ReleaseNotes
			return nil
		}
		draft, derr := ed.DraftReleaseNotes()
		if derr != nil {
			return derr
		}
		snap := ed.BuildReleaseSnapshot()
		notesFile = snap.ReleaseNotesFile
		provenance = draft.Provenance
		markdown = draft.Markdown
		return nil
	})
	if err != nil {
		return nil, err
	}
	msg := fmt.Sprintf("Saved release notes to %s.", notesFile)
	if provenance != "" {
		msg = fmt.Sprintf("Saved release notes to %s (%s).", notesFile, provenance)
	}
	return &Result{
		Text: msg,
		Structured: map[string]any{
			"file":       notesFile,
			"provenance": provenance,
			"markdown":   markdown,
		},
	}, nil
}

func releaseReviewUploadsTool(ctx context.Context, c *Call) (*Result, error) {
	var args struct{}
	if err := c.Decode(&args); err != nil {
		return nil, err
	}
	var review editor.ReleaseUploadReviewSnapshot
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		if ed.Project == nil {
			return editor.ErrNoProject
		}
		if !ed.BuildReleaseScreenOpen() {
			if err := ed.OpenBuildReleaseScreen(); err != nil {
				return err
			}
		}
		if err := ed.ReviewReleaseUploads(); err != nil {
			return err
		}
		review = ed.BuildReleaseSnapshot().UploadReview
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &Result{
		Text: fmt.Sprintf("Opened Review uploads with %d artifact(s). Nothing was uploaded: store uploads always require the person to confirm in Review uploads, whatever the permission profile.", len(review.Items)),
		Structured: map[string]any{
			"open":              review.Open,
			"uploaded":          false,
			"requiresConfirm":   true,
			"permissionProfile": review.PermissionProfile,
			"releaseNotesFile":  review.ReleaseNotesFile,
			"artifactCount":     len(review.Items),
			"items":             review.Items,
		},
	}, nil
}
