package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/EggyFly/Wiki-X/internal/config"
	"github.com/EggyFly/Wiki-X/internal/store"
)

const (
	RightRead        = store.RightRead
	RightEdit        = store.RightEdit
	RightReview      = store.RightReview
	RightUpload      = store.RightUpload
	RightManageUsers = store.RightManageUsers

	SessionCookieName = "wiki_x_session"
	csrfCookieName    = "wiki_x_csrf"
	oauthStateCookie  = "wiki_x_oauth_state"
	totpTicketCookie  = "wiki_x_totp_ticket"
	totpTicketTTL     = 5 * time.Minute
)

var ErrNoSession = errors.New("no session")

type sessionContextKey struct{}

func WithSession(ctx context.Context, session *store.SessionData) context.Context {
	return context.WithValue(ctx, sessionContextKey{}, session)
}

func SessionFromContext(ctx context.Context) *store.SessionData {
	session, _ := ctx.Value(sessionContextKey{}).(*store.SessionData)
	return session
}

type Manager struct {
	store        *store.Store
	cfg          config.AuthConfig
	github       *githubOAuth
	loginMu      sync.Mutex
	loginHistory map[string][]time.Time
	proxies      []*net.IPNet
	totpKey      []byte
}

func New(cfg config.AuthConfig, data *store.Store) *Manager {
	m := &Manager{store: data, cfg: cfg, loginHistory: make(map[string][]time.Time)}
	if cfg.GitHubEnabled() {
		m.github = newGitHubOAuth(cfg.GitHub)
	}
	for _, item := range cfg.TrustedProxies {
		if _, network, err := net.ParseCIDR(item); err == nil {
			m.proxies = append(m.proxies, network)
		}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err == nil {
		m.totpKey = key
	}
	return m
}

func (m *Manager) IssueTOTPTicket(w http.ResponseWriter, userID int64) error {
	if m.totpKey == nil {
		return errors.New("totp ticket key is not ready")
	}
	payload := strconv.FormatInt(userID, 10) + "." + strconv.FormatInt(time.Now().Add(totpTicketTTL).Unix(), 10)
	mac := hmac.New(sha256.New, m.totpKey)
	mac.Write([]byte(payload))
	value := payload + "." + hex.EncodeToString(mac.Sum(nil))
	http.SetCookie(w, &http.Cookie{
		Name:     totpTicketCookie,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(totpTicketTTL / time.Second),
	})
	return nil
}

func (m *Manager) ReadTOTPTicket(r *http.Request) (int64, bool) {
	cookie, err := r.Cookie(totpTicketCookie)
	if err != nil || cookie.Value == "" || len(m.totpKey) == 0 {
		return 0, false
	}
	index := strings.LastIndex(cookie.Value, ".")
	if index <= 0 || index == len(cookie.Value)-1 {
		return 0, false
	}
	payload, signature := cookie.Value[:index], cookie.Value[index+1:]
	mac := hmac.New(sha256.New, m.totpKey)
	mac.Write([]byte(payload))
	want := hex.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(want), []byte(signature)) != 1 {
		return 0, false
	}
	userIDText, expiresText, found := strings.Cut(payload, ".")
	if !found {
		return 0, false
	}
	userID, err := strconv.ParseInt(userIDText, 10, 64)
	if err != nil || userID <= 0 {
		return 0, false
	}
	expires, err := strconv.ParseInt(expiresText, 10, 64)
	if err != nil || time.Now().Unix() >= expires {
		return 0, false
	}
	return userID, true
}

func (m *Manager) ClearTOTPTicket(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     totpTicketCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   m.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (m *Manager) GitHubEnabled() bool {
	return m.github != nil
}

func (m *Manager) GitHubAuthURL(state string) string {
	if m.github == nil {
		return ""
	}
	return m.github.AuthURL(state)
}

func (m *Manager) CompleteGitHub(ctx context.Context, code string) (*GitHubProfile, error) {
	if m.github == nil {
		return nil, errors.New("github oauth is not configured")
	}
	return m.github.Exchange(ctx, code)
}

func (m *Manager) Store() *store.Store {
	return m.store
}

func (m *Manager) SecureCookies() bool {
	return m.cfg.SecureCookies
}

func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, 2, 32*1024, 2, 32)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, 32*1024, 2, 2,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash)), nil
}

func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false
	}
	if memory < 8*1024 || memory > 256*1024 || iterations < 1 || iterations > 10 || parallelism < 1 || parallelism > 16 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) < 16 || len(want) > 64 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(want)))
	return subtle.ConstantTimeCompare(want, got) == 1
}

var (
	dummyHashOnce sync.Once
	dummyHash     string
)

func VerifyLogin(user *store.User, password string) bool {
	if user == nil || user.PasswordHash == "" {
		dummyHashOnce.Do(func() {
			dummyHash, _ = HashPassword("wiki_x-dummy-password")
		})
		VerifyPassword(dummyHash, password)
		return false
	}
	return VerifyPassword(user.PasswordHash, password)
}

func NewSessionToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

func HashEmailToken(token string) string {
	return hashToken(token)
}

func NewEmailToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

func NewCSRFToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (m *Manager) CreateSession(ctx context.Context, w http.ResponseWriter, r *http.Request, user *store.User) (string, error) {
	token, tokenHash, err := NewSessionToken()
	if err != nil {
		return "", err
	}
	csrfToken, err := NewCSRFToken()
	if err != nil {
		return "", err
	}
	ttl := m.cfg.SessionDuration()
	if ttl <= 0 {
		ttl = 14 * 24 * time.Hour
	}
	expiresAt := time.Now().Add(ttl)
	if err := m.store.CreateSession(ctx, tokenHash, user.ID, csrfToken, expiresAt, r.UserAgent(), m.ClientIP(r)); err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
	})
	return csrfToken, nil
}

func (m *Manager) CurrentSession(ctx context.Context, r *http.Request) (*store.SessionData, string, error) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil, "", ErrNoSession
	}
	session, err := m.store.GetSession(ctx, hashToken(cookie.Value))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, "", ErrNoSession
		}
		return nil, "", err
	}
	return session, cookie.Value, nil
}

func (m *Manager) Logout(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
		_ = m.store.DeleteSession(ctx, hashToken(cookie.Value))
	}
	m.ClearSessionCookie(w)
}

func (m *Manager) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   m.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (m *Manager) LoginCSRF(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(csrfCookieName); err == nil && len(cookie.Value) >= 32 {
		return cookie.Value
	}
	token, err := NewCSRFToken()
	if err != nil {
		return ""
	}
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(10 * time.Minute),
	})
	return token
}

func (m *Manager) CheckLoginCSRF(r *http.Request) bool {
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	form := r.FormValue("csrf_token")
	return form != "" && subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(form)) == 1
}

func (m *Manager) CheckCSRF(r *http.Request, session *store.SessionData) bool {
	if session == nil || session.CSRFToken == "" {
		return false
	}
	form := r.FormValue("csrf_token")
	return form != "" && subtle.ConstantTimeCompare([]byte(session.CSRFToken), []byte(form)) == 1
}

func (m *Manager) NewOAuthState(w http.ResponseWriter) (string, error) {
	return m.newOAuthState(w, "login")
}

func (m *Manager) NewLinkOAuthState(w http.ResponseWriter) (string, error) {
	return m.newOAuthState(w, "link")
}

func (m *Manager) newOAuthState(w http.ResponseWriter, kind string) (string, error) {
	random, err := NewCSRFToken()
	if err != nil {
		return "", err
	}
	state := kind + "." + random
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(10 * time.Minute),
	})
	return state, nil
}

func (m *Manager) CheckOAuthState(w http.ResponseWriter, r *http.Request) (string, bool) {
	cookie, err := r.Cookie(oauthStateCookie)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	state := r.URL.Query().Get("state")
	ok := state != "" && subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) == 1
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   m.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	if !ok {
		return "", false
	}
	kind, _, _ := strings.Cut(cookie.Value, ".")
	return kind, true
}

func (m *Manager) AllowLogin(key string) bool {
	m.loginMu.Lock()
	defer m.loginMu.Unlock()
	now := time.Now()
	window := 5 * time.Minute
	attempts := m.loginHistory[key]
	valid := attempts[:0]
	for _, at := range attempts {
		if now.Sub(at) < window {
			valid = append(valid, at)
		}
	}
	if len(valid) >= 10 {
		m.loginHistory[key] = valid
		return false
	}
	m.loginHistory[key] = valid
	m.trimLoginHistoryLocked(now, window)
	return true
}

func (m *Manager) RecordLoginFailure(key string) {
	m.loginMu.Lock()
	defer m.loginMu.Unlock()
	now := time.Now()
	m.loginHistory[key] = append(m.loginHistory[key], now)
	m.trimLoginHistoryLocked(now, 5*time.Minute)
}

func (m *Manager) trimLoginHistoryLocked(now time.Time, window time.Duration) {
	if len(m.loginHistory) <= 10000 {
		return
	}
	for key, attempts := range m.loginHistory {
		if len(attempts) == 0 || now.Sub(attempts[len(attempts)-1]) > window {
			delete(m.loginHistory, key)
		}
	}
	for len(m.loginHistory) > 10000 {
		for key := range m.loginHistory {
			delete(m.loginHistory, key)
			break
		}
	}
}

func (m *Manager) RecordLoginSuccess(key string) {
	m.loginMu.Lock()
	defer m.loginMu.Unlock()
	delete(m.loginHistory, key)
}

func (m *Manager) CleanupSessions(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = m.store.DeleteExpiredSessions(ctx)
			_ = m.store.DeleteExpiredEmailTokens(ctx)
			_ = m.store.CleanupOIDC(ctx)
		}
	}
}

func (m *Manager) ClientIP(r *http.Request) string {
	direct := remoteHost(r)
	if !m.ipTrusted(direct) {
		return direct
	}
	// 反代之后 XFF 才有意义, 从右往左取第一个不受信的地址, 全受信时退回最左
	forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(forwarded) - 1; i >= 0; i-- {
		ip := strings.TrimSpace(forwarded[i])
		if ip == "" {
			continue
		}
		if !m.ipTrusted(ip) {
			return ip
		}
	}
	for _, ip := range forwarded {
		if ip = strings.TrimSpace(ip); ip != "" {
			return ip
		}
	}
	return direct
}

func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (m *Manager) ipTrusted(value string) bool {
	if len(m.proxies) == 0 {
		return false
	}
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return false
	}
	for _, network := range m.proxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
