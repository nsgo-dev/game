package cloudsave

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/nsgo-dev/game/pkg/online/accounts/token"
)

// Options are what a Service needs besides its Config.
type Options struct {
	// Store keeps the slots. Required.
	Store Store
	// Tokens verifies the bearer tokens of HTTP requests: a token.Verifier built from the account service's key set (token.NewVerifier). Required. The Go methods take the account ID directly; Handler takes it from the token's subject.
	Tokens *token.Verifier
	// Logger receives errors outside the service's contract, such as a failing database. Nil discards them.
	Logger *slog.Logger
	// Now returns the current time, used to stamp versions; time.Now when nil.
	Now func() time.Time
}

// Service is the cloud save service. It is safe for concurrent use.
type Service struct {
	cfg    Config
	store  Store
	tokens *token.Verifier
	log    *slog.Logger
	now    func() time.Time
}

// New returns a service with the configuration and options.
func New(cfg Config, opts Options) (*Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if opts.Store == nil || opts.Tokens == nil {
		return nil, errors.New("cloudsave: need a Store and a token verifier")
	}
	cfg.AllowedOrigins = slices.Clone(cfg.AllowedOrigins)
	s := &Service{cfg: cfg, store: opts.Store, tokens: opts.Tokens, log: opts.Logger, now: opts.Now}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s, nil
}

// Config returns the service's configuration.
func (s *Service) Config() Config {
	c := s.cfg
	c.AllowedOrigins = slices.Clone(s.cfg.AllowedOrigins)
	return c
}

// Load returns the current version of the account's slot with its data, or ErrNotFound when the slot was never written or is deleted.
func (s *Service) Load(ctx context.Context, account, slot string) (Version, error) {
	if err := checkNames(account, slot); err != nil {
		return Version{}, err
	}
	v, err := s.store.Head(ctx, account, slot)
	if err != nil {
		return Version{}, s.storeErr("load", err)
	}
	if v.Deleted {
		return Version{}, ErrNotFound
	}
	return v, nil
}

// Save writes data as the slot's next version when m holds, returning the new version without data. It returns ErrSlotTooLarge for data over Config.MaxSlotBytes, ErrChanged when m does not hold and ErrTooManySlots for a new slot beyond Config.MaxSlots; then nothing is written.
func (s *Service) Save(ctx context.Context, account, slot string, data []byte, m Match) (Version, error) {
	if err := checkNames(account, slot); err != nil {
		return Version{}, err
	}
	if len(data) > s.cfg.MaxSlotBytes {
		return Version{}, fmt.Errorf("%w: %d bytes, limit %d", ErrSlotTooLarge, len(data), s.cfg.MaxSlotBytes)
	}
	v, err := s.store.Append(ctx, account, slot, s.write(Write{Data: data, Match: m}))
	if err != nil {
		return Version{}, s.storeErr("save", err)
	}
	return v, nil
}

// Delete deletes the slot when m holds, adding a version that marks it deleted; its earlier versions stay listed and can be restored. Deleting a slot that does not exist succeeds when m allows it (MatchAny or MatchHash("")). It returns ErrChanged when m does not hold.
func (s *Service) Delete(ctx context.Context, account, slot string, m Match) error {
	if err := checkNames(account, slot); err != nil {
		return err
	}
	_, err := s.store.Append(ctx, account, slot, s.write(Write{Delete: true, Match: m}))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return s.storeErr("delete", err)
}

// List returns the current version of each of the account's live slots, sorted by name and without data.
func (s *Service) List(ctx context.Context, account string) ([]Version, error) {
	if err := validateAccount(account); err != nil {
		return nil, err
	}
	heads, err := s.store.Slots(ctx, account)
	if err != nil {
		return nil, s.storeErr("list", err)
	}
	out := heads[:0]
	for _, h := range heads {
		if !h.Deleted {
			out = append(out, h)
		}
	}
	return out, nil
}

// Versions lists the kept versions of the slot, deletions included, newest first and without data. It returns ErrNotFound for a slot with none.
func (s *Service) Versions(ctx context.Context, account, slot string) ([]Version, error) {
	if err := checkNames(account, slot); err != nil {
		return nil, err
	}
	vs, err := s.store.Versions(ctx, account, slot)
	if err != nil {
		return nil, s.storeErr("versions", err)
	}
	return vs, nil
}

// Version returns one kept version of the slot with its data, or ErrNotFound.
func (s *Service) Version(ctx context.Context, account, slot string, number int64) (Version, error) {
	if err := checkNames(account, slot); err != nil {
		return Version{}, err
	}
	v, err := s.store.Version(ctx, account, slot, number)
	if err != nil {
		return Version{}, s.storeErr("version", err)
	}
	return v, nil
}

// Restore writes the data of kept version number as the slot's next version when m holds, so the slot holds that payload again, deleted or not. A version that is a deletion deletes the slot. The restored bytes are the old payload unchanged; liveops.CloudClient.Restore re-stamps a CloudSyncer payload instead, so devices see the restore as the newest copy.
func (s *Service) Restore(ctx context.Context, account, slot string, number int64, m Match) (Version, error) {
	old, err := s.Version(ctx, account, slot, number)
	if err != nil {
		return Version{}, err
	}
	if old.Deleted {
		if err := s.Delete(ctx, account, slot, m); err != nil {
			return Version{}, err
		}
		return s.headMeta(ctx, account, slot)
	}
	return s.Save(ctx, account, slot, old.Data, m)
}

// Purge removes the slot and all its versions for good.
func (s *Service) Purge(ctx context.Context, account, slot string) error {
	if err := checkNames(account, slot); err != nil {
		return err
	}
	return s.storeErr("purge", s.store.Purge(ctx, account, slot))
}

func (s *Service) headMeta(ctx context.Context, account, slot string) (Version, error) {
	v, err := s.store.Head(ctx, account, slot)
	if err != nil {
		return Version{}, s.storeErr("load", err)
	}
	return withoutData(v), nil
}

func (s *Service) write(w Write) Write {
	w.Created = s.now()
	w.Keep = s.cfg.KeepVersions
	w.MaxSlots = s.cfg.MaxSlots
	return w
}

// storeErr passes the store's contract errors and context errors on, and logs any other.
func (s *Service) storeErr(op string, err error) error {
	switch {
	case err == nil, errors.Is(err, ErrNotFound), errors.Is(err, ErrChanged), errors.Is(err, ErrTooManySlots),
		errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	}
	s.log.Error("cloudsave: store failed", "op", op, "err", err)
	return fmt.Errorf("cloudsave: %s: %w", op, err)
}

func checkNames(account, slot string) error {
	if err := validateAccount(account); err != nil {
		return err
	}
	return ValidateSlot(slot)
}
