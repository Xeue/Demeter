// Package auth provides user accounts (argon2id), sessions, role gating, an
// audit log, and HTTP/WebSocket middleware. Electron's IPC was implicitly local
// and unauthenticated; a network-hosted control plane that can reboot cards and
// blast config must authenticate and audit, so this gates both HTTP routes and
// the WebSocket upgrade.
package auth

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Xeue/Demeter/internal/store"
)

// Role is a user's permission level.
type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
)

// AtLeast reports whether r satisfies the required role (admin satisfies all).
func (r Role) AtLeast(required Role) bool {
	if r == RoleAdmin {
		return true
	}
	return r == required
}

// User is an account.
type User struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"passwordHash"`
	Role         Role      `json:"role"`
	Disabled     bool      `json:"disabled"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Session is a logged-in session.
type Session struct {
	Token    string    `json:"token"`
	Username string    `json:"username"`
	Role     Role      `json:"role"`
	Expiry   time.Time `json:"expiry"`
}

var (
	// ErrInvalidCredentials is returned for a bad username/password or disabled user.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrLocked is returned when an account is temporarily locked after failures.
	ErrLocked = errors.New("account temporarily locked, try again shortly")
	// ErrExists is returned when creating a user that already exists.
	ErrExists = errors.New("user already exists")
)

// Notice is a one-time generated-credentials notice, surfaced in the GUI on
// first run so a desktop user (who has no terminal) can see and change the
// auto-generated password. The plaintext only exists in memory for the run in
// which it was generated.
type Notice struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Auth holds the user/session state.
type Auth struct {
	dataDir    string
	audit      *Audit
	sessionTTL time.Duration
	secure     bool
	now        func() time.Time

	mu       sync.Mutex
	users    map[string]*User
	sessions map[string]*Session
	fails    map[string]*failTracker
	notice   *Notice
}

// SetNotice records a generated-credentials notice to surface once in the GUI.
func (a *Auth) SetNotice(username, password string) {
	a.mu.Lock()
	a.notice = &Notice{Username: username, Password: password}
	a.mu.Unlock()
}

// Notice returns a copy of the pending notice, or nil.
func (a *Auth) Notice() *Notice {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.notice == nil {
		return nil
	}
	cp := *a.notice
	return &cp
}

// ClearNotice discards the pending notice (dismissed or password changed).
func (a *Auth) ClearNotice() {
	a.mu.Lock()
	a.notice = nil
	a.mu.Unlock()
}

type failTracker struct {
	count int
	until time.Time
}

const (
	maxFails    = 5
	failWindow  = time.Minute
	defaultTTL  = 7 * 24 * time.Hour
	usersFile   = "users.json"
	sessionFile = "sessions.json"
)

// New loads (or initialises) users/sessions from dataDir/data and starts the
// audit log under dataDir/logs. secure marks session cookies Secure (TLS).
func New(dataDir string, audit *Audit, secure bool, now func() time.Time) (*Auth, error) {
	if now == nil {
		now = time.Now
	}
	a := &Auth{
		dataDir: dataDir, audit: audit, sessionTTL: defaultTTL, secure: secure, now: now,
		users: map[string]*User{}, sessions: map[string]*Session{}, fails: map[string]*failTracker{},
	}
	var users map[string]*User
	if err := store.ReadJSON(a.usersPath(), &users); err != nil {
		slog.Warn("auth: could not read users.json", "err", err)
	}
	if users != nil {
		a.users = users
	}
	var sessions map[string]*Session
	if err := store.ReadJSON(a.sessionsPath(), &sessions); err == nil && sessions != nil {
		for tok, s := range sessions {
			if s.Expiry.After(now()) {
				a.sessions[tok] = s
			}
		}
	}
	return a, nil
}

func (a *Auth) usersPath() string    { return filepath.Join(a.dataDir, "data", usersFile) }
func (a *Auth) sessionsPath() string { return filepath.Join(a.dataDir, "data", sessionFile) }

// Bootstrap creates an initial admin if no users exist. It uses
// DEMETER_ADMIN_USER / DEMETER_ADMIN_PASS if set, otherwise generates a random
// password and logs it once.
func (a *Auth) Bootstrap() error {
	a.mu.Lock()
	empty := len(a.users) == 0
	a.mu.Unlock()
	if !empty {
		return nil
	}
	user := os.Getenv("DEMETER_ADMIN_USER")
	if user == "" {
		user = "admin"
	}
	pass := os.Getenv("DEMETER_ADMIN_PASS")
	generated := false
	if pass == "" {
		pass = randomToken(12)
		generated = true
	}
	if err := a.CreateUser(user, pass, RoleAdmin); err != nil {
		return err
	}
	if generated {
		slog.Warn("ADMIN BOOTSTRAP: created initial admin account - change the password immediately",
			"username", user, "password", pass)
		a.SetNotice(user, pass)
		a.writeInitialPassword(user, pass)
	} else {
		slog.Info("ADMIN BOOTSTRAP: created initial admin from environment", "username", user)
	}
	return nil
}

// writeInitialPassword records a freshly generated admin password to a 0600 file
// in the data dir, so a headless operator (who may never see stdout/journald)
// can retrieve it for first login: `cat <dataDir>/INITIAL_ADMIN_PASSWORD`.
// Best-effort — a failure to write it is logged but not fatal.
func (a *Auth) writeInitialPassword(user, pass string) {
	path := filepath.Join(a.dataDir, "INITIAL_ADMIN_PASSWORD")
	body := fmt.Sprintf("username: %s\npassword: %s\n\n"+
		"This is the auto-generated first-run admin login. Change the password in\n"+
		"the GUI, then delete this file.\n", user, pass)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		slog.Warn("could not write INITIAL_ADMIN_PASSWORD file", "path", path, "err", err)
		return
	}
	slog.Warn("ADMIN BOOTSTRAP: password also saved to file", "path", path)
}

// Login verifies credentials and, on success, returns the user.
func (a *Auth) Login(username, password string) (*User, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	a.mu.Lock()
	if ft := a.fails[username]; ft != nil && ft.count >= maxFails && a.now().Before(ft.until) {
		a.mu.Unlock()
		return nil, ErrLocked
	}
	u := a.users[username]
	a.mu.Unlock()

	if u == nil || u.Disabled || !VerifyPassword(u.PasswordHash, password) {
		a.recordFailure(username)
		return nil, ErrInvalidCredentials
	}
	a.clearFailure(username)
	return u, nil
}

func (a *Auth) recordFailure(username string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ft := a.fails[username]
	if ft == nil || a.now().After(ft.until) {
		ft = &failTracker{}
		a.fails[username] = ft
	}
	ft.count++
	ft.until = a.now().Add(failWindow)
}

func (a *Auth) clearFailure(username string) {
	a.mu.Lock()
	delete(a.fails, username)
	a.mu.Unlock()
}

// CreateUser adds a new user (error if it exists).
func (a *Auth) CreateUser(username, password string, role Role) error {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" || password == "" {
		return errors.New("username and password required")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	a.mu.Lock()
	if _, ok := a.users[username]; ok {
		a.mu.Unlock()
		return ErrExists
	}
	now := a.now().UTC()
	a.users[username] = &User{Username: username, PasswordHash: hash, Role: role, CreatedAt: now, UpdatedAt: now}
	a.mu.Unlock()
	return a.persistUsers()
}

// DeleteUser removes a user.
func (a *Auth) DeleteUser(username string) error {
	username = strings.ToLower(strings.TrimSpace(username))
	a.mu.Lock()
	delete(a.users, username)
	a.mu.Unlock()
	return a.persistUsers()
}

// SetRole changes a user's role.
func (a *Auth) SetRole(username string, role Role) error {
	username = strings.ToLower(strings.TrimSpace(username))
	a.mu.Lock()
	if u := a.users[username]; u != nil {
		u.Role = role
		u.UpdatedAt = a.now().UTC()
	}
	a.mu.Unlock()
	return a.persistUsers()
}

// ResetPassword sets a new password for a user.
func (a *Auth) ResetPassword(username, password string) error {
	username = strings.ToLower(strings.TrimSpace(username))
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	a.mu.Lock()
	if u := a.users[username]; u != nil {
		u.PasswordHash = hash
		u.UpdatedAt = a.now().UTC()
	}
	if a.notice != nil && a.notice.Username == username {
		a.notice = nil // password changed -> the generated-credentials notice is done
	}
	a.mu.Unlock()
	return a.persistUsers()
}

// SetDisabled enables/disables a user.
func (a *Auth) SetDisabled(username string, disabled bool) error {
	username = strings.ToLower(strings.TrimSpace(username))
	a.mu.Lock()
	if u := a.users[username]; u != nil {
		u.Disabled = disabled
		u.UpdatedAt = a.now().UTC()
	}
	a.mu.Unlock()
	return a.persistUsers()
}

// User returns a copy of a user by name, or nil.
func (a *Auth) User(username string) *User {
	username = strings.ToLower(strings.TrimSpace(username))
	a.mu.Lock()
	defer a.mu.Unlock()
	if u := a.users[username]; u != nil {
		cp := *u
		return &cp
	}
	return nil
}

// ListUsers returns a copy of users (without password hashes) for the admin UI.
func (a *Auth) ListUsers() []User {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]User, 0, len(a.users))
	for _, u := range a.users {
		cp := *u
		cp.PasswordHash = ""
		out = append(out, cp)
	}
	return out
}

func (a *Auth) persistUsers() error {
	a.mu.Lock()
	snap := make(map[string]*User, len(a.users))
	for k, v := range a.users {
		cp := *v
		snap[k] = &cp
	}
	a.mu.Unlock()
	return store.WriteJSON(a.usersPath(), snap)
}

// Audit returns the audit logger (may be nil).
func (a *Auth) Audit() *Audit { return a.audit }

func randomToken(n int) string {
	return tokenString(n)
}
