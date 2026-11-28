package liveops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/game/pkg/online/accounts"
	"github.com/nsgo-dev/nsgo/core"
)

var ErrSignedOut = errors.New("liveops: signed out")

// SecretStore is the small keychain interface Account needs. core.Secrets can
// be adapted without making the HTTP client depend on a particular host.
type SecretStore interface {
	Get(string) (string, bool)
	Set(string, string) error
	Delete(string) error
}

type AccountOptions struct {
	URL           string
	Audience      string
	Secrets       SecretStore
	SecretsKey    string
	Client        *http.Client
	RefreshMargin time.Duration
}

type Account struct {
	mu         sync.Mutex
	opts       AccountOptions
	session    accounts.Secret
	accountID  string
	token      accounts.Secret
	expires    time.Time
	refreshing bool
	wait       chan struct{}
}

func NewAccount(opts AccountOptions) *Account {
	if opts.Client == nil {
		opts.Client = http.DefaultClient
	}
	if opts.RefreshMargin <= 0 {
		opts.RefreshMargin = 30 * time.Second
	}
	a := &Account{opts: opts, wait: make(chan struct{})}
	if opts.Secrets != nil {
		if raw, ok := opts.Secrets.Get(opts.SecretsKey); ok {
			a.session = accounts.NewSecret(raw)
		}
	}
	return a
}
func (a *Account) ProfileID() string { a.mu.Lock(); defer a.mu.Unlock(); return a.accountID }
func (a *Account) Token(ctx context.Context) (string, error) {
	for {
		a.mu.Lock()
		if a.session.IsZero() {
			a.mu.Unlock()
			return "", ErrSignedOut
		}
		if !a.token.IsZero() && time.Until(a.expires) > a.opts.RefreshMargin {
			tok := a.token.Reveal()
			a.mu.Unlock()
			return tok, nil
		}
		if !a.refreshing {
			a.refreshing = true
			session := a.session.Reveal()
			a.mu.Unlock()
			c, err := a.refresh(ctx, session)
			a.mu.Lock()
			a.refreshing = false
			close(a.wait)
			a.wait = make(chan struct{})
			if err == nil {
				tok := c.Token
				a.mu.Unlock()
				return tok, nil
			}
			a.mu.Unlock()
			return "", err
		}
		wait := a.wait
		a.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}
func (a *Account) refresh(ctx context.Context, session string) (credentials, error) {
	var out credentials
	err := a.request(ctx, "/v1/refresh", map[string]string{"session": session, "audience": a.opts.Audience}, &out)
	if err != nil {
		if errors.Is(err, ErrSignedOut) {
			a.SignOut(context.Background())
		}
		return credentials{}, err
	}
	a.mu.Lock()
	a.accountID = out.Account
	a.token = accounts.NewSecret(out.Token)
	a.expires = out.Expires
	a.mu.Unlock()
	return out, nil
}
func (a *Account) SignInEmail(ctx context.Context, email, password string) error {
	var c credentials
	if err := a.request(ctx, "/v1/signin/email", map[string]string{"email": email, "password": password}, &c); err != nil {
		return err
	}
	return a.accept(c)
}
func (a *Account) SignOut(ctx context.Context) error {
	a.mu.Lock()
	sess := a.session.Reveal()
	a.session = accounts.Secret{}
	a.token = accounts.Secret{}
	a.accountID = ""
	a.mu.Unlock()
	if a.opts.Secrets != nil && a.opts.SecretsKey != "" {
		_ = a.opts.Secrets.Delete(a.opts.SecretsKey)
	}
	if sess == "" {
		return nil
	}
	return a.request(ctx, "/v1/signout", map[string]string{"session": sess}, nil)
}
func (a *Account) accept(c credentials) error {
	if c.Session == "" || c.Token == "" {
		return errors.New("liveops: account service returned incomplete credentials")
	}
	a.mu.Lock()
	a.session = accounts.NewSecret(c.Session)
	a.accountID = c.Account
	a.token = accounts.NewSecret(c.Token)
	a.expires = c.Expires
	a.mu.Unlock()
	if a.opts.Secrets != nil && a.opts.SecretsKey != "" {
		if err := a.opts.Secrets.Set(a.opts.SecretsKey, c.Session); err != nil {
			return err
		}
	}
	return nil
}

type credentials struct {
	Account, Session, Token string
	Expires                 time.Time
}

func (a *Account) request(ctx context.Context, path string, body any, out any) error {
	base := strings.TrimRight(a.opts.URL, "/")
	if base == "" {
		return errors.New("liveops: account URL is empty")
	}
	data, _ := json.Marshal(body)
	method := http.MethodPost
	if path == "/v1/me" {
		method = http.MethodGet
		data = nil
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.opts.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrSignedOut
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("liveops: account service: %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return err
	}
	return nil
}

// NewAccountForScope constructs an Account using NSGo's protected keychain.
// Secrets are accessed through the supplied scope's secure store and never
// mirrored into ordinary storage.
func NewAccountForScope(cx *core.Scope, opts AccountOptions) *Account {
	if cx == nil {
		return NewAccount(opts)
	}
	opts.Secrets = coreSecretStore{secrets: cx.Secrets()}
	if opts.SecretsKey == "" {
		opts.SecretsKey = "session"
	}
	return NewAccount(opts)
}

type coreSecretStore struct{ secrets core.Secrets }

func (s coreSecretStore) Get(key string) (string, bool) { v, ok, _ := s.secrets.Get(key); return v, ok }
func (s coreSecretStore) Set(key, value string) error   { return s.secrets.Set(key, value) }
func (s coreSecretStore) Delete(key string) error       { return s.secrets.Delete(key) }

// Register creates an email account and signs it in.
func (a *Account) Register(ctx context.Context, email, password string) error {
	var c credentials
	if err := a.request(ctx, "/v1/register", map[string]string{"email": email, "password": password}, &c); err != nil {
		return err
	}
	return a.accept(c)
}

// LinkEmail adds an email/password login to the current account.
func (a *Account) LinkEmail(ctx context.Context, email, password string) error {
	return a.authedRequest(ctx, "/v1/link/email", map[string]string{"email": email, "password": password}, nil)
}

// LinkIdentity links a provider credential to the current account.
func (a *Account) LinkIdentity(ctx context.Context, provider, credential string) error {
	return a.authedRequest(ctx, "/v1/link/identity", map[string]string{"provider": provider, "credential": credential}, nil)
}

// AccountConflict identifies a provider identity already owned by another account.
type AccountConflict struct{ Provider string }

func (e *AccountConflict) Error() string {
	return fmt.Sprintf("liveops: identity %q belongs to another account", e.Provider)
}

// Profile returns the signed-in account profile from the account service.
func (a *Account) Profile(ctx context.Context) (accounts.Profile, error) {
	var out struct {
		Account, Email, Created string
		Guest                   bool
		Identities              []accounts.Identity
	}
	if err := a.authedRequest(ctx, "/v1/me", nil, &out); err != nil {
		return accounts.Profile{}, err
	}
	created, _ := time.Parse(time.RFC3339Nano, out.Created)
	return accounts.Profile{Account: accounts.Account{ID: out.Account, Email: out.Email, Created: created}, Identities: out.Identities}, nil
}

func (a *Account) authedRequest(ctx context.Context, path string, body, out any) error {
	tok, err := a.serviceToken(ctx)
	if err != nil {
		return err
	}
	return a.requestWithToken(ctx, path, body, out, tok)
}

func (a *Account) serviceToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	session := a.session.Reveal()
	a.mu.Unlock()
	if session == "" {
		return "", ErrSignedOut
	}
	var c credentials
	if err := a.request(ctx, "/v1/refresh", map[string]string{"session": session, "audience": accounts.ServiceAudience}, &c); err != nil {
		return "", err
	}
	return c.Token, nil
}
func (a *Account) requestWithToken(ctx context.Context, path string, body, out any, token string) error {
	base := strings.TrimRight(a.opts.URL, "/")
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	method := http.MethodPost
	if path == "/v1/me" {
		method = http.MethodGet
		data = nil
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := a.opts.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict && strings.Contains(path, "identity") {
		return &AccountConflict{}
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrSignedOut
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("liveops: account service: %s", resp.Status)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
