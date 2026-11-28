package accounts

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrNotFound reports an account, session or identity that does not exist.
	ErrNotFound = errors.New("accounts: not found")
	// ErrTaken reports an email address or identity that belongs to another account.
	ErrTaken = errors.New("accounts: already belongs to another account")
	// ErrAlreadyLinked reports an account that already has an email address, or already has an identity of the provider.
	ErrAlreadyLinked = errors.New("accounts: account already linked")
	// ErrInvalidCredentials reports a wrong email address or password, an unknown or revoked session, or an identity credential its verifier refused. It never says which.
	ErrInvalidCredentials = errors.New("accounts: invalid credentials")
	// ErrInvalidEmail reports an email address that is not a plain address.
	ErrInvalidEmail = errors.New("accounts: invalid email address")
	// ErrWeakPassword reports a password shorter than the configured minimum or longer than MaxPasswordBytes.
	ErrWeakPassword = errors.New("accounts: password too short or too long")
	// ErrUnknownProvider reports an identity provider the service has no verifier for.
	ErrUnknownProvider = errors.New("accounts: unknown identity provider")
	// ErrUnknownAudience reports a token request for an audience that is neither the game's, ServiceAudience nor one of Config.Audiences.
	ErrUnknownAudience = errors.New("accounts: unknown token audience")
	// ErrGuestsDisabled reports CreateGuest on a service configured with DisableGuests.
	ErrGuestsDisabled = errors.New("accounts: guest accounts are disabled")
	// ErrLastCredential reports an attempt to remove the account's last way to sign in.
	ErrLastCredential = errors.New("accounts: cannot remove the last sign-in method")
)

// Account is one player's account.
type Account struct {
	// ID is the account's permanent random ID, the key of everything the game keeps for the player.
	ID string
	// Email is the account's normalized email address, empty for none.
	Email string
	// Created is when the account was created, to the millisecond.
	Created time.Time
}

// Identity is an account's identity at a provider, such as a Game Center player or a Steam user.
type Identity struct {
	// Provider names the provider, such as "gamecenter", "playgames", "steam" or "google".
	Provider string
	// Subject is the provider's stable ID for the player.
	Subject string
}

// Session is one signed-in device. The store keeps only a hash of the session's secret.
type Session struct {
	// ID is the session's random ID, the part of the device's credential before the dot.
	ID string
	// AccountID is the account the session signs in to.
	AccountID string
	// SecretHash is the SHA-256 of the session's secret, in hex.
	SecretHash string
	// Created is when the session was opened, to the millisecond.
	Created time.Time
}

// Store keeps the service's accounts, identities and sessions. Implementations must be safe for concurrent use and enforce the uniqueness the methods state atomically, so that two devices racing to claim one email address or identity cannot both succeed. Package storetest checks a Store against this contract.
type Store interface {
	// CreateAccount stores a new account with the password hash (empty for none; only with an Email) and links the identities to it, all or nothing. It returns ErrTaken when the ID, email address or an identity already exists, or when two identities share a provider.
	CreateAccount(ctx context.Context, a Account, passwordHash string, identities ...Identity) error
	// Account returns the account with id, or ErrNotFound.
	Account(ctx context.Context, id string) (Account, error)
	// AccountByEmail returns the account with the normalized email address and its password hash, or ErrNotFound.
	AccountByEmail(ctx context.Context, email string) (Account, string, error)
	// SetEmail gives an account without an email address one and its password hash. It returns ErrNotFound for no such account, ErrAlreadyLinked when the account has an email address, ErrInvalidEmail for an empty address, and ErrTaken when another account has this one.
	SetEmail(ctx context.Context, id, email, passwordHash string) error
	// SetPasswordHash replaces the password hash of an account with an email address, or returns ErrNotFound.
	SetPasswordHash(ctx context.Context, id, passwordHash string) error
	// LinkIdentity links an identity to an account. It returns nil when the identity is already linked to this account, ErrNotFound for no such account, ErrTaken when another account has the identity, and ErrAlreadyLinked when the account has another identity of the provider.
	LinkIdentity(ctx context.Context, id string, identity Identity, at time.Time) error
	// UnlinkIdentity removes the account's identity for provider, or returns ErrNotFound when it has none.
	UnlinkIdentity(ctx context.Context, id, provider string) error
	// AccountByIdentity returns the account an identity is linked to, or ErrNotFound.
	AccountByIdentity(ctx context.Context, identity Identity) (Account, error)
	// Identities returns the identities linked to an account, sorted by provider; none for an unknown account.
	Identities(ctx context.Context, id string) ([]Identity, error)
	// CreateSession stores a new session of an existing account. It returns ErrNotFound for no such account and ErrTaken when the session ID exists.
	CreateSession(ctx context.Context, s Session) error
	// Session returns the session with id, or ErrNotFound.
	Session(ctx context.Context, id string) (Session, error)
	// DeleteSession deletes the session with id, or returns ErrNotFound.
	DeleteSession(ctx context.Context, id string) error
}
