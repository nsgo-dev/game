// Package accounts is a self-hostable player account service: guest accounts that upgrade to email sign-in or to a linked OAuth or platform identity (Game Center, Play Games, Steam), and short-lived tokens a game server verifies offline.
//
// # Accounts, sessions and tokens
//
// An account is an ID. A game keys everything it keeps for a player (saves, progress, purchases) by that ID, so upgrading a guest by linking an email address or an identity keeps the player's data: the ID does not change.
//
// Every sign-in (CreateGuest, Register, SignInEmail, SignInIdentity) opens a session on the device and returns Credentials:
//   - Session is a long-lived secret the device stores to get new tokens with Refresh, revoked by SignOut. It is the only credential a guest has.
//   - Token is a short-lived signed token (package token) the game sends to its server, which verifies it with a token.Verifier and no network call. It expires after the configured TTL. A token is good only for the audience it names: sign-ins return one for the game's servers, and RefreshFor returns one for the service itself (ServiceAudience) or for another service in Config.Audiences, such as cloud save, so a game server cannot replay a player's token against those.
//
// # Identities
//
// A provider such as "gamecenter", "playgames", "steam" or "google" is an IdentityVerifier the service is given: it turns a credential from the device (a signed Game Center identity, a Play Games server auth code, a Steam session ticket, an OpenID Connect ID token) into the provider's stable subject for the player. Linking an identity to an account lets a second device sign in to the same account with SignInIdentity.
//
// # Secrets
//
// Passwords are stored only as argon2id hashes (a memory-hard hash). Session secrets are stored only as SHA-256 hashes. Credentials hold their secrets as Secret values, which print, log and marshal as "[redacted]"; the service never logs a password, a session secret, a token or an identity credential.
//
// # Storage
//
// The service keeps its records in a Store: NewMemoryStore for tests and prototypes, or the SQL stores of the sqlitestore and pgstore packages, which share sqlstore. Package storetest checks any Store against the contract.
//
// # Configuration
//
// Config is a versioned YAML file (ParseConfig, Config.Marshal); Service.Handler serves the service over HTTP with JSON bodies.
package accounts
