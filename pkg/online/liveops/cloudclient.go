package liveops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxCloudPayloadBytes bounds a payload CloudClient reads, the largest slot the self-hosted service can be configured to keep (64 MiB).
const maxCloudPayloadBytes = 64 << 20

// maxCloudListingBytes bounds a JSON listing CloudClient reads.
const maxCloudListingBytes = 8 << 20

// CloudClientOptions configures NewCloudClient.
type CloudClientOptions struct {
	// URL is the base URL of the cloud save service (package cloudsave), such as "https://saves.example.com". It must use https, except on a loopback host (localhost, 127.0.0.1, ::1) during development. Required.
	URL string
	// Token returns a current bearer token from the account service for the signed-in player and the cloud save service's audience ("cloudsave" by default: POST /v1/refresh with that audience), getting a new one when the last has expired. The token a sign-in returns is for game servers, and the service refuses it. Every device signed in to the same account reads and writes the same slots. Required.
	Token func(ctx context.Context) (string, error)
	// HTTPClient sends the requests; http.DefaultClient when nil. The client uses a copy that does not follow redirects. On the web its requests go through the browser's fetch, so the page's origin must be in the service's allowedOrigins.
	HTTPClient *http.Client
	// Now stamps payloads Restore re-writes; time.Now when nil.
	Now func() time.Time
}

// CloudClient is the CloudSync of the self-hosted cloud save service in package cloudsave, over HTTPS. It implements ConditionalCloudSync, so a CloudSyncer on it never overwrites another device's upload, and adds the service's version history: Versions, LoadVersion, Restore and Purge.
//
// Its errors wrap the liveops sentinels: ErrSlotNotFound, ErrCloudChanged when a conditional write lost to another device, ErrSlotTooLarge (with the service's limit in the message), ErrTooManySlots, and ErrCloudUnavailable when the network, the service or the token fails, which a sync treats as offline. A done context's error is returned as it is. A CloudClient is safe for concurrent use.
type CloudClient struct {
	base  *url.URL
	token func(context.Context) (string, error)
	http  *http.Client
	now   func() time.Time
}

var _ ConditionalCloudSync = (*CloudClient)(nil)

// CloudVersion describes one kept version of a slot in the service's history.
type CloudVersion struct {
	// Number counts the slot's versions from 1; each write or delete adds one.
	Number int64
	// Deleted marks a version that deleted the slot.
	Deleted bool
	// Hash is the PayloadHash of the version's payload, empty for a deletion.
	Hash string
	// Size is the payload's length in bytes.
	Size int64
	// Created is when the service stored the version.
	Created time.Time
}

// NewCloudClient returns a client for opts, or an error when the URL or the token source is missing or invalid.
func NewCloudClient(opts CloudClientOptions) (*CloudClient, error) {
	if opts.Token == nil {
		return nil, errors.New("liveops: cloud client needs a Token source")
	}
	u, err := url.Parse(opts.URL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("liveops: cloud client URL %q is not a base URL such as https://saves.example.com", opts.URL)
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname())) {
		return nil, fmt.Errorf("liveops: cloud client URL %q must use https", opts.URL)
	}
	// Request paths, already escaped, are appended to the base's text.
	u.Path, u.RawPath = strings.TrimSuffix(u.Path, "/"), strings.TrimSuffix(u.RawPath, "/")
	hc := http.DefaultClient
	if opts.HTTPClient != nil {
		hc = opts.HTTPClient
	}
	// The service never redirects. Following one would send the token elsewhere and could turn another resource's 2xx into a slot's payload, so a redirect is returned as it is and reported as an error.
	noRedirect := *hc
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c := &CloudClient{base: u, token: opts.Token, http: &noRedirect, now: opts.Now}
	if c.now == nil {
		c.now = time.Now
	}
	return c, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Load implements CloudSync. It checks the payload against the service's ETag.
func (c *CloudClient) Load(ctx context.Context, slot string) ([]byte, error) {
	data, _, err := c.getPayload(ctx, "load", slot, slotPath(slot))
	return data, err
}

// Save implements CloudSync: it replaces the slot whatever it holds.
func (c *CloudClient) Save(ctx context.Context, slot string, data []byte) error {
	return c.put(ctx, slot, data, nil)
}

// SaveIf implements ConditionalCloudSync.
func (c *CloudClient) SaveIf(ctx context.Context, slot string, data []byte, match string) error {
	return c.put(ctx, slot, data, precondition(match))
}

// Delete implements CloudSync. The service keeps the slot's history, so Restore can bring it back.
func (c *CloudClient) Delete(ctx context.Context, slot string) error {
	_, err := c.do(ctx, "delete", slot, http.MethodDelete, slotPath(slot), nil, nil, 0)
	return err
}

// DeleteIf implements ConditionalCloudSync.
func (c *CloudClient) DeleteIf(ctx context.Context, slot, match string) error {
	_, err := c.do(ctx, "delete", slot, http.MethodDelete, slotPath(slot), nil, precondition(match), 0)
	return err
}

// List implements CloudSync: the names of the player's live slots, in byte order.
func (c *CloudClient) List(ctx context.Context) ([]string, error) {
	var resp struct {
		Slots []cloudVersionJSON `json:"slots"`
	}
	if err := c.getJSON(ctx, "list", "", "/v1/saves", &resp); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(resp.Slots))
	for _, v := range resp.Slots {
		out = append(out, v.Slot)
	}
	return out, nil
}

// Versions lists the kept versions of slot, newest first, deletions included. It returns ErrSlotNotFound for a slot with no history.
func (c *CloudClient) Versions(ctx context.Context, slot string) ([]CloudVersion, error) {
	var resp struct {
		Versions []cloudVersionJSON `json:"versions"`
	}
	if err := c.getJSON(ctx, "list versions of", slot, slotPath(slot)+"/versions", &resp); err != nil {
		return nil, err
	}
	out := make([]CloudVersion, 0, len(resp.Versions))
	for _, v := range resp.Versions {
		t, err := time.Parse(time.RFC3339Nano, v.Created)
		if err != nil {
			return nil, fmt.Errorf("liveops: list versions of %q: %w: created %q", slot, ErrCloudUnavailable, v.Created)
		}
		out = append(out, CloudVersion{Number: v.Version, Deleted: v.Deleted, Hash: v.Hash, Size: v.Size, Created: t})
	}
	return out, nil
}

// LoadVersion returns the payload of kept version number of slot, as Load returns the current one. It returns ErrSlotNotFound when the version is not kept or is a deletion.
func (c *CloudClient) LoadVersion(ctx context.Context, slot string, number int64) ([]byte, error) {
	data, deleted, err := c.getPayload(ctx, "load version of", slot, versionPath(slot, number))
	if err == nil && deleted {
		return nil, fmt.Errorf("liveops: load version %d of %q: %w: the version is a deletion", number, slot, ErrSlotNotFound)
	}
	return data, err
}

// Restore makes kept version number the slot's current copy, as a new version written only while the slot still holds the copy Restore read (ErrCloudChanged otherwise). Restoring a deletion deletes the slot.
//
// A payload written by CloudSyncer is re-stamped with the next revision and the current time, so every device sees the restore as the newest copy: one whose copy is unchanged since its last sync downloads it on the next sync, and one with unsynced changes gets a conflict its policy resolves. Other payloads are restored byte for byte.
func (c *CloudClient) Restore(ctx context.Context, slot string, number int64) error {
	old, deleted, err := c.getPayload(ctx, "restore", slot, versionPath(slot, number))
	if err != nil {
		return err
	}
	cur, err := c.Load(ctx, slot)
	exists := err == nil
	if err != nil && !errors.Is(err, ErrSlotNotFound) {
		return err
	}
	match := ""
	if exists {
		match = PayloadHash(cur)
	}
	if deleted {
		if !exists {
			return nil
		}
		return c.DeleteIf(ctx, slot, match)
	}
	payload := old
	if isEnvelope(old) {
		v, err := DecodeCloudSave(old)
		if err != nil {
			return fmt.Errorf("liveops: restore version %d of %q: %w", number, slot, err)
		}
		rev := v.Revision
		if exists {
			// A current copy that is plain or unreadable has no revision to follow.
			if cv, err := DecodeCloudSave(cur); err == nil {
				rev = max(rev, cv.Revision)
			}
		}
		payload, err = EncodeCloudSave(SaveVersion{Exists: true, Revision: rev + 1, Modified: c.now(), Data: v.Data})
		if err != nil {
			return fmt.Errorf("liveops: restore version %d of %q: %w", number, slot, err)
		}
	}
	return c.SaveIf(ctx, slot, payload, match)
}

// Purge removes slot and its whole history from the service for good; a slot that does not exist is not an error.
func (c *CloudClient) Purge(ctx context.Context, slot string) error {
	_, err := c.do(ctx, "purge", slot, http.MethodDelete, slotPath(slot)+"/versions", nil, nil, 0)
	return err
}

type cloudVersionJSON struct {
	Slot    string `json:"slot"`
	Version int64  `json:"version"`
	Deleted bool   `json:"deleted"`
	Hash    string `json:"hash"`
	Size    int64  `json:"size"`
	Created string `json:"created"`
}

func slotPath(slot string) string { return "/v1/saves/" + url.PathEscape(slot) }

func versionPath(slot string, number int64) string {
	return slotPath(slot) + "/versions/" + strconv.FormatInt(number, 10)
}

// precondition returns the header that makes a write conditional on match.
func precondition(match string) http.Header {
	if match == "" {
		return http.Header{"If-None-Match": {"*"}}
	}
	return http.Header{"If-Match": {strconv.Quote(match)}}
}

func (c *CloudClient) put(ctx context.Context, slot string, data []byte, h http.Header) error {
	if h == nil {
		h = http.Header{}
	}
	h.Set("Content-Type", "application/octet-stream")
	_, err := c.do(ctx, "save", slot, http.MethodPut, slotPath(slot), data, h, 0)
	return err
}

// getPayload GETs a payload and checks it against its ETag. deleted reports a version that is a deletion.
func (c *CloudClient) getPayload(ctx context.Context, op, slot, path string) (data []byte, deleted bool, err error) {
	resp, err := c.do(ctx, op, slot, http.MethodGet, path, nil, nil, maxCloudPayloadBytes)
	if errors.Is(err, errDeletedVersion) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if tag := resp.etag; tag != "" && tag != PayloadHash(resp.body) {
		return nil, false, fmt.Errorf("liveops: %s %q: %w: payload does not match its ETag", op, slot, ErrCloudUnavailable)
	}
	return resp.body, false, nil
}

func (c *CloudClient) getJSON(ctx context.Context, op, slot, path string, v any) error {
	resp, err := c.do(ctx, op, slot, http.MethodGet, path, nil, nil, maxCloudListingBytes)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(resp.body, v); err != nil {
		return fmt.Errorf("liveops: %s %q: %w: malformed response: %v", op, slot, ErrCloudUnavailable, err)
	}
	return nil
}

// errDeletedVersion reports a 410 for a version that is a deletion.
var errDeletedVersion = errors.New("liveops: version is a deletion")

type cloudResponse struct {
	body []byte
	etag string
}

// do sends one request and maps the service's errors onto the liveops sentinels. limit bounds the body read from a successful response.
func (c *CloudClient) do(ctx context.Context, op, slot, method, path string, body []byte, h http.Header, limit int64) (cloudResponse, error) {
	what := op
	if slot != "" {
		what = fmt.Sprintf("%s %q", op, slot)
	}
	if slot == "" && path != "/v1/saves" {
		return cloudResponse{}, fmt.Errorf("liveops: %s: empty slot name", what)
	}
	if slot == "." || slot == ".." {
		return cloudResponse{}, fmt.Errorf("liveops: %s: the cloud save service cannot store a slot named %q", what, slot)
	}
	if err := ctx.Err(); err != nil {
		return cloudResponse{}, err
	}
	tok, err := c.token(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return cloudResponse{}, ctx.Err()
		}
		return cloudResponse{}, fmt.Errorf("liveops: %s: %w: no account token: %v", what, ErrCloudUnavailable, err)
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base.String()+path, rd)
	if err != nil {
		return cloudResponse{}, fmt.Errorf("liveops: %s: %w", what, err)
	}
	for k, vs := range h {
		req.Header[k] = vs
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return cloudResponse{}, ctx.Err()
		}
		return cloudResponse{}, fmt.Errorf("liveops: %s: %w: %v", what, ErrCloudUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 == 2 {
		if limit == 0 {
			return cloudResponse{}, nil
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		if err != nil {
			if ctx.Err() != nil {
				return cloudResponse{}, ctx.Err()
			}
			return cloudResponse{}, fmt.Errorf("liveops: %s: %w: %v", what, ErrCloudUnavailable, err)
		}
		if int64(len(data)) > limit {
			return cloudResponse{}, fmt.Errorf("liveops: %s: %w: response over %d bytes", what, ErrCloudUnavailable, limit)
		}
		var tag string
		if et := resp.Header.Get("ETag"); et != "" {
			if tag, err = strconv.Unquote(et); err != nil {
				return cloudResponse{}, fmt.Errorf("liveops: %s: %w: malformed ETag %q", what, ErrCloudUnavailable, et)
			}
		}
		return cloudResponse{body: data, etag: tag}, nil
	}
	var e struct {
		Error string `json:"error"`
		Limit int64  `json:"limit"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&e)
	switch {
	case resp.StatusCode == http.StatusNotFound && e.Error == "not_found":
		return cloudResponse{}, fmt.Errorf("liveops: %s: %w", what, ErrSlotNotFound)
	case resp.StatusCode == http.StatusGone && e.Error == "deleted":
		return cloudResponse{}, errDeletedVersion
	case resp.StatusCode == http.StatusPreconditionFailed:
		return cloudResponse{}, fmt.Errorf("liveops: %s: %w", what, ErrCloudChanged)
	case resp.StatusCode == http.StatusRequestEntityTooLarge:
		if e.Limit > 0 {
			return cloudResponse{}, fmt.Errorf("liveops: %s: %w: %d bytes, the service keeps at most %d", what, ErrSlotTooLarge, len(body), e.Limit)
		}
		return cloudResponse{}, fmt.Errorf("liveops: %s: %w: %d bytes", what, ErrSlotTooLarge, len(body))
	case resp.StatusCode == http.StatusConflict && e.Error == "too_many_slots":
		return cloudResponse{}, fmt.Errorf("liveops: %s: %w", what, ErrTooManySlots)
	case resp.StatusCode == http.StatusBadRequest:
		return cloudResponse{}, fmt.Errorf("liveops: %s: request refused by the cloud save service (%s)", what, e.Error)
	case resp.StatusCode == http.StatusUnauthorized:
		return cloudResponse{}, fmt.Errorf("liveops: %s: %w: the account token was refused", what, ErrCloudUnavailable)
	}
	return cloudResponse{}, fmt.Errorf("liveops: %s: %w: HTTP %d %s", what, ErrCloudUnavailable, resp.StatusCode, e.Error)
}
