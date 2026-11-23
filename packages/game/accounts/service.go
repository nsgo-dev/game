package accounts

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/mail"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/nsgo-dev/game/packages/game/accounts/token"
)

// IdentityVerifier checks a credential a device got from an identity provider and returns the provider's stable subject for the player. A verifier for Game Center checks the signed identity against Apple's certificate, one for Play Games exchanges the server auth code, one for Steam authenticates the session ticket with the Steam Web API, and one for an OAuth provider verifies the OpenID Connect ID token.
//
// The credential is a secret: a verifier must not log it or put it in its errors. The service never shows a verifier's error to the caller; it returns ErrInvalidCredentials.
type IdentityVerifier interface {
	VerifyIdentity(ctx context.Context, credential string) (subject string, err error)
}

// IdentityVerifierFunc adapts a function to IdentityVerifier.
type IdentityVerifierFunc func(ctx context.Context, credential string) (string, error)

// VerifyIdentity calls f.
func (f IdentityVerifierFunc) VerifyIdentity(ctx context.Context, credential string) (string, error) {
	return f(ctx, credential)
}

// Options are what a Service needs besides its Config.
type Options struct {
	// Store keeps the records; required.
	Store Store
	// Key signs tokens; required. Keep it in a PEM file read with token.ParsePrivateKey.
	Key ed25519.PrivateKey
	// KeySet holds public keys the service still accepts besides Key's, such as the key it signed with before a rotation; nil accepts only Key's. Its issuer and audience must match the configuration. The service verifies bearer tokens with Key's public key and these, and serves them all at GET /v1/keys, so tokens signed with an old key keep working until the key is removed from the set.
	KeySet *token.KeySet
	// Identities are the identity providers by name, such as "gamecenter" or "steam". A name is 1 to 32 characters of a-z, 0-9, '.', '_' and '-', starting with a letter or digit.
	Identities map[string]IdentityVerifier
	// Logger receives account events and store errors, never secrets; nil discards them.
	Logger *slog.Logger
	// Now is the service's clock; nil uses time.Now.
	Now func() time.Time
	// Rand fills IDs, secrets and salts; nil uses crypto/rand. The service serializes its reads, so a reader that is not safe for concurrent use, such as a deterministic one in a test, may be given.
	Rand io.Reader
	// MaxConcurrentHashes bounds how many password hashes run at once, each using the configured argon2id memory; 0 uses GOMAXPROCS.
	MaxConcurrentHashes int
}

// Service is the account service. It is safe for concurrent use.
type Service struct {
	cfg       Config
	store     Store
	signer    *token.Signer
	keySet    *token.KeySet
	verifier  *token.Verifier
	providers map[string]IdentityVerifier
	log       *slog.Logger
	now       func() time.Time
	rand      io.Reader
	hashes    chan struct{}

	dummyOnce sync.Once
	dummy     string
}

// Credentials are what a sign-in gives a device.
type Credentials struct {
	// AccountID is the account signed in to.
	AccountID string
	// Session is the device's long-lived secret for Refresh and SignOut. Store it on the device: for a guest it is the only way back into the account.
	Session Secret
	// Token is the short-lived token the game sends its server.
	Token Secret
	// Expires is when Token expires.
	Expires time.Time
	// Created reports whether the sign-in created the account.
	Created bool
}

// Profile is an account with its linked identities.
type Profile struct {
	Account
	// Identities are the account's linked identities, sorted by provider.
	Identities []Identity
}

// Guest reports whether the account has neither an email address nor an identity, so only its sessions can sign in to it.
func (p Profile) Guest() bool { return p.Email == "" && len(p.Identities) == 0 }

// New returns a service with the configuration and options. The token signer is built from cfg's issuer, audience and TTL and opts.Key.
func New(cfg Config, opts Options) (*Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if opts.Store == nil {
		return nil, errors.New("accounts: no store")
	}
	signer, err := token.NewSigner(opts.Key, cfg.Issuer, cfg.Audience, time.Duration(cfg.TokenTTL))
	if err != nil {
		return nil, fmt.Errorf("accounts: %w", err)
	}
	s := &Service{cfg: cfg, store: opts.Store, signer: signer, providers: map[string]IdentityVerifier{}, log: opts.Logger, now: opts.Now, rand: opts.Rand}
	for name, v := range opts.Identities {
		if !validProvider(name) {
			return nil, fmt.Errorf("accounts: invalid provider name %q", name)
		}
		if v == nil {
			return nil, fmt.Errorf("accounts: provider %q has no verifier", name)
		}
		s.providers[name] = v
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.rand == nil {
		s.rand = rand.Reader
	} else {
		s.rand = &lockedReader{r: s.rand}
	}
	n := opts.MaxConcurrentHashes
	if n <= 0 {
		n = runtime.GOMAXPROCS(0)
	}
	s.hashes = make(chan struct{}, n)
	signer.Now = s.now
	signer.Rand = s.rand
	s.keySet = signer.KeySet()
	if opts.KeySet != nil {
		if s.keySet, err = s.keySet.Merge(opts.KeySet); err != nil {
			return nil, fmt.Errorf("accounts: key set: %w", err)
		}
	}
	s.verifier, err = token.NewVerifier(s.keySet)
	if err != nil {
		return nil, err
	}
	s.verifier.Now = s.now
	return s, nil
}

// Config returns the service's configuration.
func (s *Service) Config() Config { return s.cfg }

// KeySet returns the public key set a game server verifies the service's tokens with: the signing key's public half first, then the keys of Options.KeySet.
func (s *Service) KeySet() *token.KeySet {
	ks := *s.keySet
	ks.Keys = append([]token.PublicKey(nil), s.keySet.Keys...)
	return &ks
}

// Authenticate verifies a token the service issued and returns its claims.
func (s *Service) Authenticate(tok string) (token.Claims, error) { return s.verifier.Verify(tok) }

// CreateGuest creates an account with no email address or identity and signs the device in to it.
func (s *Service) CreateGuest(ctx context.Context) (Credentials, error) {
	if s.cfg.DisableGuests {
		return Credentials{}, ErrGuestsDisabled
	}
	a, err := s.newAccount()
	if err != nil {
		return Credentials{}, err
	}
	if err := s.store.CreateAccount(ctx, a, ""); err != nil {
		return Credentials{}, s.storeError("create account", err)
	}
	s.log.InfoContext(ctx, "accounts: account created", "account", a.ID, "method", "guest")
	return s.openSession(ctx, a.ID, true)
}

// Register creates an account with an email address and password and signs the device in to it. It returns ErrTaken when the address belongs to an account.
func (s *Service) Register(ctx context.Context, email, password string) (Credentials, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return Credentials{}, err
	}
	hash, err := s.hashPassword(ctx, password)
	if err != nil {
		return Credentials{}, err
	}
	a, err := s.newAccount()
	if err != nil {
		return Credentials{}, err
	}
	a.Email = email
	if err := s.store.CreateAccount(ctx, a, hash); err != nil {
		return Credentials{}, s.storeError("create account", err)
	}
	s.log.InfoContext(ctx, "accounts: account created", "account", a.ID, "method", "email")
	return s.openSession(ctx, a.ID, true)
}

// LinkEmail gives an account (a guest, or one signed in with an identity) an email address and password, keeping its ID and so its data. It returns ErrAlreadyLinked when the account has an address and ErrTaken when another account has this one.
func (s *Service) LinkEmail(ctx context.Context, accountID, email, password string) error {
	email, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	hash, err := s.hashPassword(ctx, password)
	if err != nil {
		return err
	}
	if err := s.store.SetEmail(ctx, accountID, email, hash); err != nil {
		return s.storeError("set email", err)
	}
	s.log.InfoContext(ctx, "accounts: email linked", "account", accountID)
	return nil
}

// SignInEmail signs a device in with an email address and password. A wrong address or password returns ErrInvalidCredentials and takes as long as a right one. A password hashed with a cost other than the configured one is rehashed.
func (s *Service) SignInEmail(ctx context.Context, email, password string) (Credentials, error) {
	if err := s.acquireHash(ctx); err != nil {
		return Credentials{}, err
	}
	a, ok, err := s.checkPassword(ctx, email, password)
	s.releaseHash()
	if err != nil {
		return Credentials{}, err
	}
	if !ok {
		s.log.InfoContext(ctx, "accounts: sign-in refused", "method", "email")
		return Credentials{}, ErrInvalidCredentials
	}
	s.log.InfoContext(ctx, "accounts: signed in", "account", a.ID, "method", "email")
	return s.openSession(ctx, a.ID, false)
}

// checkPassword looks up the address and checks the password, rehashing it when its cost is outdated. The caller holds a hash slot.
func (s *Service) checkPassword(ctx context.Context, email, password string) (Account, bool, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		s.dummyVerify(password)
		return Account{}, false, nil
	}
	a, hash, err := s.store.AccountByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) {
		s.dummyVerify(password)
		return Account{}, false, nil
	}
	if err != nil {
		return Account{}, false, s.storeError("account by email", err)
	}
	ok, params, err := VerifyPassword(password, hash)
	if err != nil {
		s.log.ErrorContext(ctx, "accounts: stored password hash is malformed", "account", a.ID)
		return Account{}, false, nil
	}
	if ok && params != s.cfg.Password.Argon2id {
		if fresh, err := HashPassword(password, s.cfg.Password.Argon2id, s.rand); err == nil {
			if err := s.store.SetPasswordHash(ctx, a.ID, fresh); err != nil {
				s.log.ErrorContext(ctx, "accounts: rehash failed", "account", a.ID, "err", err)
			}
		}
	}
	return a, ok, nil
}

// dummyVerify spends a password check's time on a hash of the configured cost, so an unknown address answers as slowly as a wrong password.
func (s *Service) dummyVerify(password string) {
	s.dummyOnce.Do(func() {
		s.dummy, _ = HashPassword("dummy password", s.cfg.Password.Argon2id, s.rand)
	})
	_, _, _ = VerifyPassword(password, s.dummy)
}

// SignInIdentity signs a device in with a credential from provider. An identity linked to no account gets a new account (Credentials.Created), so a player can start with a platform identity as well as upgrade to one.
func (s *Service) SignInIdentity(ctx context.Context, provider, credential string) (Credentials, error) {
	identity, err := s.verifyIdentity(ctx, provider, credential)
	if err != nil {
		return Credentials{}, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		a, err := s.store.AccountByIdentity(ctx, identity)
		if err == nil {
			s.log.InfoContext(ctx, "accounts: signed in", "account", a.ID, "method", "identity", "provider", provider)
			return s.openSession(ctx, a.ID, false)
		}
		if !errors.Is(err, ErrNotFound) {
			return Credentials{}, s.storeError("account by identity", err)
		}
		a, err = s.newAccount()
		if err != nil {
			return Credentials{}, err
		}
		err = s.store.CreateAccount(ctx, a, "", identity)
		if err == nil {
			s.log.InfoContext(ctx, "accounts: account created", "account", a.ID, "method", "identity", "provider", provider)
			return s.openSession(ctx, a.ID, true)
		}
		if !errors.Is(err, ErrTaken) {
			return Credentials{}, s.storeError("create account", err)
		}
		// Another device created the account for this identity first: sign in to it.
	}
	return Credentials{}, s.storeError("sign in with identity", ErrTaken)
}

// LinkIdentity links the identity a credential from provider proves to an account, keeping the account's ID and so its data. Another device then signs in to the account with SignInIdentity. It returns ErrTaken when another account has the identity and ErrAlreadyLinked when the account has another identity of the provider.
func (s *Service) LinkIdentity(ctx context.Context, accountID, provider, credential string) error {
	identity, err := s.verifyIdentity(ctx, provider, credential)
	if err != nil {
		return err
	}
	if err := s.store.LinkIdentity(ctx, accountID, identity, s.timestamp()); err != nil {
		return s.storeError("link identity", err)
	}
	s.log.InfoContext(ctx, "accounts: identity linked", "account", accountID, "provider", provider)
	return nil
}

// Refresh returns a new token for a device's session. The session stays the same.
func (s *Service) Refresh(ctx context.Context, session Secret) (Credentials, error) {
	sess, err := s.checkSession(ctx, session)
	if err != nil {
		return Credentials{}, err
	}
	tok, claims, err := s.signer.Issue(sess.AccountID)
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{AccountID: sess.AccountID, Session: session, Token: NewSecret(tok), Expires: claims.Expires}, nil
}

// SignOut revokes a device's session. Tokens already issued stay valid until they expire.
func (s *Service) SignOut(ctx context.Context, session Secret) error {
	sess, err := s.checkSession(ctx, session)
	if err != nil {
		return err
	}
	if err := s.store.DeleteSession(ctx, sess.ID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrInvalidCredentials
		}
		return s.storeError("delete session", err)
	}
	s.log.InfoContext(ctx, "accounts: signed out", "account", sess.AccountID)
	return nil
}

// Profile returns an account and its identities.
func (s *Service) Profile(ctx context.Context, accountID string) (Profile, error) {
	a, err := s.store.Account(ctx, accountID)
	if err != nil {
		return Profile{}, s.storeError("account", err)
	}
	ids, err := s.store.Identities(ctx, accountID)
	if err != nil {
		return Profile{}, s.storeError("identities", err)
	}
	return Profile{Account: a, Identities: ids}, nil
}

// NormalizeEmail trims and lowercases a plain email address ("ada@example.com"), refusing display names, angle brackets and addresses longer than 254 bytes with ErrInvalidEmail.
func NormalizeEmail(email string) (string, error) {
	email = strings.TrimSpace(email)
	if email == "" || len(email) > 254 || !utf8.ValidString(email) {
		return "", ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Name != "" || addr.Address != email {
		return "", ErrInvalidEmail
	}
	return strings.ToLower(email), nil
}

// sessionIDLen is the length of a session ID: 16 random bytes in hex.
const sessionIDLen = 32

// openSession opens a session of the account and issues its first token.
func (s *Service) openSession(ctx context.Context, accountID string, created bool) (Credentials, error) {
	id, err := s.randomHex(16)
	if err != nil {
		return Credentials{}, err
	}
	var secret [32]byte
	if _, err := io.ReadFull(s.rand, secret[:]); err != nil {
		return Credentials{}, fmt.Errorf("accounts: secret: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(secret[:])
	sess := Session{ID: id, AccountID: accountID, SecretHash: hashSecret(encoded), Created: s.timestamp()}
	if err := s.store.CreateSession(ctx, sess); err != nil {
		return Credentials{}, s.storeError("create session", err)
	}
	tok, claims, err := s.signer.Issue(accountID)
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{AccountID: accountID, Session: NewSecret(id + "." + encoded), Token: NewSecret(tok), Expires: claims.Expires, Created: created}, nil
}

// checkSession finds the session a device's secret names and checks the secret against its hash.
func (s *Service) checkSession(ctx context.Context, session Secret) (Session, error) {
	id, secret, ok := strings.Cut(session.Reveal(), ".")
	if !ok || len(id) != sessionIDLen || len(secret) != base64.RawURLEncoding.EncodedLen(32) {
		return Session{}, ErrInvalidCredentials
	}
	if _, err := hex.DecodeString(id); err != nil {
		return Session{}, ErrInvalidCredentials
	}
	sess, err := s.store.Session(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, s.storeError("session", err)
	}
	if subtle.ConstantTimeCompare([]byte(hashSecret(secret)), []byte(sess.SecretHash)) != 1 {
		return Session{}, ErrInvalidCredentials
	}
	return sess, nil
}

// hashSecret is the stored form of a session secret. The secret is 256 random bits, so a fast hash suffices.
func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// verifyIdentity runs the provider's verifier on a credential.
func (s *Service) verifyIdentity(ctx context.Context, provider, credential string) (Identity, error) {
	v, ok := s.providers[provider]
	if !ok {
		return Identity{}, ErrUnknownProvider
	}
	if credential == "" || len(credential) > maxCredentialBytes {
		return Identity{}, ErrInvalidCredentials
	}
	subject, err := v.VerifyIdentity(ctx, credential)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Identity{}, ctxErr
		}
		// The verifier's error may quote the credential, so it is not logged.
		s.log.InfoContext(ctx, "accounts: identity refused", "provider", provider)
		return Identity{}, ErrInvalidCredentials
	}
	if subject == "" || len(subject) > maxSubjectBytes || !utf8.ValidString(subject) {
		s.log.ErrorContext(ctx, "accounts: identity verifier returned an invalid subject", "provider", provider)
		return Identity{}, ErrInvalidCredentials
	}
	return Identity{Provider: provider, Subject: subject}, nil
}

const (
	maxCredentialBytes = 16 << 10
	maxSubjectBytes    = 255
)

// validProvider reports whether name is a valid provider name.
func validProvider(name string) bool {
	if name == "" || len(name) > 32 {
		return false
	}
	for i, c := range []byte(name) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case (c == '.' || c == '_' || c == '-') && i > 0:
		default:
			return false
		}
	}
	return true
}

// hashPassword checks the password rules and hashes it, waiting for a hash slot.
func (s *Service) hashPassword(ctx context.Context, password string) (string, error) {
	if utf8.RuneCountInString(password) < s.cfg.Password.MinLength || len(password) > MaxPasswordBytes {
		return "", ErrWeakPassword
	}
	if err := s.acquireHash(ctx); err != nil {
		return "", err
	}
	defer s.releaseHash()
	return HashPassword(password, s.cfg.Password.Argon2id, s.rand)
}

func (s *Service) acquireHash(ctx context.Context) error {
	select {
	case s.hashes <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) releaseHash() { <-s.hashes }

// newAccount returns an account with a new random ID created now.
func (s *Service) newAccount() (Account, error) {
	id, err := s.randomHex(16)
	if err != nil {
		return Account{}, err
	}
	return Account{ID: id, Created: s.timestamp()}, nil
}

func (s *Service) randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(s.rand, b); err != nil {
		return "", fmt.Errorf("accounts: random: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// lockedReader serializes reads from a reader that may not be safe for concurrent use.
type lockedReader struct {
	mu sync.Mutex
	r  io.Reader
}

func (l *lockedReader) Read(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.Read(p)
}

// timestamp is now in UTC to the millisecond, the precision stores keep.
func (s *Service) timestamp() time.Time { return s.now().UTC().Truncate(time.Millisecond) }

// storeError passes the store's contract errors through and logs any other error before returning it wrapped.
func (s *Service) storeError(op string, err error) error {
	for _, known := range []error{ErrNotFound, ErrTaken, ErrAlreadyLinked} {
		if errors.Is(err, known) {
			return known
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	s.log.Error("accounts: store failed", "op", op, "err", err)
	return fmt.Errorf("accounts: %s: %w", op, err)
}
