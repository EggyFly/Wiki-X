package oidc

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/EggyFly/Wiki-X/internal/config"
	"github.com/EggyFly/Wiki-X/internal/store"
)

type Service struct {
	store      *store.Store
	issuer     string
	key        *rsa.PrivateKey
	kid        string
	accessTTL  time.Duration
	idTokenTTL time.Duration
	refreshTTL time.Duration
	codeTTL    time.Duration
}

type TokenSet struct {
	AccessToken  string
	IDToken      string
	RefreshToken string
	ExpiresIn    int64
	Scope        string
}

func New(ctx context.Context, cfg config.OIDCConfig, data *store.Store) (*Service, error) {
	if data == nil {
		return nil, errors.New("oidc requires a database")
	}
	issuer := strings.TrimRight(strings.TrimSpace(cfg.Issuer), "/")
	if issuer == "" {
		return nil, errors.New("oidc issuer is empty")
	}
	encryptionKey, err := decodeKeyEncryptionKey(cfg.KeyEncryptionKey)
	if err != nil {
		return nil, err
	}
	key, kid, err := loadOrCreateKey(ctx, data, encryptionKey)
	if err != nil {
		return nil, err
	}
	return &Service{
		store:      data,
		issuer:     issuer,
		key:        key,
		kid:        kid,
		accessTTL:  cfg.AccessDuration(),
		idTokenTTL: cfg.IDTokenDuration(),
		refreshTTL: cfg.RefreshDuration(),
		codeTTL:    cfg.CodeDuration(),
	}, nil
}

func loadOrCreateKey(ctx context.Context, data *store.Store, encryptionKey []byte) (*rsa.PrivateKey, string, error) {
	records, err := data.ListOIDCSigningKeys(ctx)
	if err != nil {
		return nil, "", err
	}
	var active *store.OIDCSigningKey
	var activePEM string
	for _, record := range records {
		encryptedPEM := record.PrivateKeyPEM
		if !strings.HasPrefix(encryptedPEM, oidcKeyPrefix) {
			encryptedPEM, err = encryptPrivateKeyPEM(encryptedPEM, encryptionKey)
			if err != nil {
				return nil, "", err
			}
			if err := data.UpdateOIDCSigningKeyPEM(ctx, record.KID, encryptedPEM); err != nil {
				return nil, "", err
			}
		}
		if record.Active && active == nil {
			active = record
			activePEM = encryptedPEM
		}
	}
	if active != nil {
		plainPEM, err := decryptPrivateKeyPEM(activePEM, encryptionKey)
		if err != nil {
			return nil, "", err
		}
		key, err := parseKey(plainPEM)
		if err != nil {
			return nil, "", err
		}
		return key, active.KID, nil
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, "", err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, "", err
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	encryptedPEM, err := encryptPrivateKeyPEM(string(pemBytes), encryptionKey)
	if err != nil {
		return nil, "", err
	}
	created, err := data.GetOrCreateOIDCSigningKey(ctx, store.OIDCSigningKey{
		KID:           "oidc_" + randomToken(8),
		PrivateKeyPEM: encryptedPEM,
	})
	if err != nil {
		return nil, "", err
	}
	plainPEM, err := decryptPrivateKeyPEM(created.PrivateKeyPEM, encryptionKey)
	if err != nil {
		return nil, "", err
	}
	key, err = parseKey(plainPEM)
	if err != nil {
		return nil, "", err
	}
	return key, created.KID, nil
}

func parseKey(value string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, errors.New("decode oidc signing key: no PEM block")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse oidc signing key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("oidc signing key is not RSA")
	}
	return key, nil
}

func (s *Service) Discovery() map[string]any {
	return map[string]any{
		"issuer":                                s.issuer,
		"authorization_endpoint":                s.issuer + "/oauth2/authorize",
		"token_endpoint":                        s.issuer + "/oauth2/token",
		"userinfo_endpoint":                     s.issuer + "/oauth2/userinfo",
		"revocation_endpoint":                   s.issuer + "/oauth2/revoke",
		"jwks_uri":                              s.issuer + "/.well-known/jwks.json",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "profile", "email"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		"code_challenge_methods_supported":      []string{"S256"},
		"claims_supported":                      []string{"sub", "iss", "aud", "exp", "iat", "auth_time", "nonce", "preferred_username", "name", "email", "email_verified"},
	}
}

func (s *Service) JWKS() map[string]any {
	exponent := big.NewInt(int64(s.key.PublicKey.E)).Bytes()
	return map[string]any{"keys": []map[string]any{{
		"kty": "RSA",
		"use": "sig",
		"alg": "RS256",
		"kid": s.kid,
		"n":   base64.RawURLEncoding.EncodeToString(s.key.PublicKey.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(exponent),
	}}}
}

func (s *Service) Issuer() string {
	return s.issuer
}

func (s *Service) CreateAuthCode(ctx context.Context, clientID string, userID int64, redirectURI, scopes, nonce, challenge string, authTime time.Time) (string, error) {
	code := randomToken(32)
	record := store.OIDCAuthCode{
		CodeHash:      Hash(code),
		ClientID:      clientID,
		UserID:        userID,
		RedirectURI:   redirectURI,
		Scopes:        scopes,
		Nonce:         nonce,
		CodeChallenge: challenge,
		Method:        "S256",
		AuthTime:      authTime,
		ExpiresAt:     time.Now().Add(s.codeTTL),
	}
	if err := s.store.CreateOIDCAuthCode(ctx, record); err != nil {
		return "", err
	}
	return code, nil
}

func (s *Service) ExchangeCode(ctx context.Context, client *store.OIDCClient, code, redirectURI, verifier string) (*TokenSet, *store.User, error) {
	record, err := s.store.ConsumeOIDCAuthCode(ctx, Hash(code))
	if err != nil {
		return nil, nil, err
	}
	if record.ClientID != client.ClientID || record.RedirectURI != redirectURI || record.Method != "S256" {
		return nil, nil, store.ErrOIDCCodeInvalid
	}
	if VerifyPKCE(verifier, record.CodeChallenge) == false {
		return nil, nil, store.ErrOIDCCodeInvalid
	}
	user, err := s.store.UserByID(ctx, record.UserID)
	if err != nil {
		return nil, nil, err
	}
	if user.Disabled {
		return nil, nil, errors.New("account is disabled")
	}
	tokens, err := s.issueTokens(ctx, client, user, record.Scopes, record.Nonce, record.AuthTime)
	if err != nil {
		return nil, nil, err
	}
	return tokens, user, nil
}

func (s *Service) issueTokens(ctx context.Context, client *store.OIDCClient, user *store.User, scopes, nonce string, authTime time.Time) (*TokenSet, error) {
	grantID := randomToken(24)
	sessionID := randomToken(24)
	if err := s.store.CreateOIDCGrantSession(ctx, grantID, sessionID, client.ClientID, user.ID, scopes, time.Now().Add(s.refreshTTL)); err != nil {
		return nil, err
	}
	now := time.Now()
	accessClaims := map[string]any{
		"iss":       s.issuer,
		"sub":       strconv.FormatInt(user.ID, 10),
		"aud":       client.ClientID,
		"azp":       client.ClientID,
		"client_id": client.ClientID,
		"scope":     scopes,
		"sid":       sessionID,
		"grant_id":  grantID,
		"iat":       now.Unix(),
		"exp":       now.Add(s.accessTTL).Unix(),
		"jti":       randomToken(16),
		"token_use": "access",
	}
	access, err := s.sign(accessClaims)
	if err != nil {
		return nil, err
	}
	idClaims := s.userClaims(user, scopes, client.ClientID, nonce, authTime)
	idClaims["iat"] = now.Unix()
	idClaims["exp"] = now.Add(s.idTokenTTL).Unix()
	idClaims["jti"] = randomToken(16)
	idToken, err := s.sign(idClaims)
	if err != nil {
		return nil, err
	}
	refresh := randomToken(32)
	if err := s.store.CreateOIDCRefreshToken(ctx, Hash(refresh), sessionID, grantID, client.ClientID, now.Add(s.refreshTTL)); err != nil {
		return nil, err
	}
	return &TokenSet{
		AccessToken:  access,
		IDToken:      idToken,
		RefreshToken: refresh,
		ExpiresIn:    int64(s.accessTTL / time.Second),
		Scope:        scopes,
	}, nil
}

const refreshReuseGrace = 10 * time.Second

func (s *Service) Refresh(ctx context.Context, client *store.OIDCClient, refresh string) (*TokenSet, error) {
	record, err := s.store.ConsumeOIDCRefreshToken(ctx, Hash(refresh))
	if err != nil {
		return nil, err
	}
	if record.ClientID != client.ClientID || record.RevokedAt != nil || time.Now().After(record.ExpiresAt) {
		return nil, errors.New("refresh token is invalid")
	}
	if record.UsedAt != nil {
		if time.Since(*record.UsedAt) <= refreshReuseGrace {
			return nil, errors.New("refresh token is already used")
		}
		_ = s.store.RevokeOIDCSession(ctx, record.SessionID)
		return nil, errors.New("refresh token reused")
	}
	session, err := s.store.GetOIDCSession(ctx, record.SessionID)
	if err != nil || session.RevokedAt != nil || session.GrantRevokedAt != nil || time.Now().After(session.ExpiresAt) {
		return nil, errors.New("grant is revoked")
	}
	user, err := s.store.UserByID(ctx, session.UserID)
	if err != nil || user.Disabled {
		return nil, errors.New("account is unavailable")
	}
	now := time.Now()
	accessClaims := map[string]any{
		"iss":       s.issuer,
		"sub":       strconv.FormatInt(user.ID, 10),
		"aud":       client.ClientID,
		"azp":       client.ClientID,
		"client_id": client.ClientID,
		"scope":     session.Scopes,
		"sid":       session.SessionID,
		"grant_id":  session.GrantID,
		"iat":       now.Unix(),
		"exp":       now.Add(s.accessTTL).Unix(),
		"jti":       randomToken(16),
		"token_use": "access",
	}
	access, err := s.sign(accessClaims)
	if err != nil {
		return nil, err
	}
	idClaims := s.userClaims(user, session.Scopes, client.ClientID, "", time.Time{})
	idClaims["iat"] = now.Unix()
	idClaims["exp"] = now.Add(s.idTokenTTL).Unix()
	idClaims["jti"] = randomToken(16)
	idToken, err := s.sign(idClaims)
	if err != nil {
		return nil, err
	}
	nextRefresh := randomToken(32)
	if err := s.store.CreateOIDCRefreshToken(ctx, Hash(nextRefresh), session.SessionID, session.GrantID, client.ClientID, now.Add(s.refreshTTL)); err != nil {
		return nil, err
	}
	_ = s.store.TouchOIDCSession(ctx, session.SessionID)
	return &TokenSet{
		AccessToken:  access,
		IDToken:      idToken,
		RefreshToken: nextRefresh,
		ExpiresIn:    int64(s.accessTTL / time.Second),
		Scope:        session.Scopes,
	}, nil
}

func (s *Service) UserInfo(ctx context.Context, token string) (map[string]any, error) {
	claims, err := s.ParseAccess(token)
	if err != nil {
		return nil, err
	}
	sessionID := claimString(claims, "sid")
	clientID := claimString(claims, "client_id")
	userID := claimString(claims, "sub")
	client, err := s.store.GetOIDCClient(ctx, clientID)
	if err != nil || !client.Enabled {
		return nil, errors.New("client unavailable")
	}
	session, err := s.store.GetOIDCSession(ctx, sessionID)
	if err != nil || session.ClientID != clientID || strconv.FormatInt(session.UserID, 10) != userID {
		return nil, errors.New("grant revoked")
	}
	if session.RevokedAt != nil || session.GrantRevokedAt != nil || time.Now().After(session.ExpiresAt) {
		return nil, errors.New("grant revoked")
	}
	user, err := s.store.UserByID(ctx, session.UserID)
	if err != nil || user.Disabled {
		return nil, errors.New("account unavailable")
	}
	result := s.userClaims(user, session.Scopes, clientID, "", time.Now())
	delete(result, "iss")
	delete(result, "aud")
	delete(result, "exp")
	delete(result, "iat")
	delete(result, "auth_time")
	delete(result, "nonce")
	return result, nil
}

func (s *Service) Revoke(ctx context.Context, clientID, token string) {
	if claims, err := s.ParseAccess(token); err == nil && claimString(claims, "client_id") == clientID {
		if sid := claimString(claims, "sid"); sid != "" {
			_ = s.store.RevokeOIDCSession(ctx, sid)
		}
		return
	}
	record, err := s.store.GetOIDCRefreshToken(ctx, Hash(token))
	if err != nil || record.ClientID != clientID {
		return
	}
	_ = s.store.RevokeOIDCSession(ctx, record.SessionID)
}

func (s *Service) ParseAccess(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed token")
	}
	header, err := decodeJSON(parts[0])
	if err != nil {
		return nil, err
	}
	if claimString(header, "alg") != "RS256" || claimString(header, "kid") != s.kid {
		return nil, errors.New("unknown signing key")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&s.key.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
		return nil, errors.New("invalid token signature")
	}
	claims, err := decodeJSON(parts[1])
	if err != nil {
		return nil, err
	}
	if claimString(claims, "iss") != s.issuer || claimString(claims, "token_use") != "access" {
		return nil, errors.New("invalid token claims")
	}
	if exp := claimInt64(claims, "exp"); exp <= time.Now().Unix() {
		return nil, errors.New("token expired")
	}
	return claims, nil
}

func (s *Service) sign(claims map[string]any) (string, error) {
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT", "kid": s.kid})
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	message := encodedHeader + "." + encodedPayload
	digest := sha256.Sum256([]byte(message))
	signature, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return message + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (s *Service) userClaims(user *store.User, scopes, clientID, nonce string, authTime time.Time) map[string]any {
	claims := map[string]any{
		"iss": s.issuer,
		"sub": strconv.FormatInt(user.ID, 10),
		"aud": clientID,
		"azp": clientID,
	}
	if !authTime.IsZero() {
		claims["auth_time"] = authTime.Unix()
	}
	if nonce != "" {
		claims["nonce"] = nonce
	}
	scopeSet := splitScopes(scopes)
	if scopeSet["profile"] {
		claims["preferred_username"] = user.Name
		claims["name"] = user.Name
	}
	if scopeSet["email"] {
		claims["email"] = user.Email
		claims["email_verified"] = user.EmailVerified
	}
	if scopeSet["groups"] {
		claims["groups"] = user.Groups
	}
	return claims
}

func VerifyPKCE(verifier, challenge string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(expected), []byte(challenge)) == 1
}

func Hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func randomToken(size int) string {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func splitScopes(value string) map[string]bool {
	out := map[string]bool{}
	for _, scope := range strings.Fields(value) {
		out[scope] = true
	}
	return out
}

func claimString(claims map[string]any, key string) string {
	value, _ := claims[key].(string)
	return value
}

func claimInt64(claims map[string]any, key string) int64 {
	switch value := claims[key].(type) {
	case float64:
		return int64(value)
	case json.Number:
		n, _ := value.Int64()
		return n
	case int64:
		return value
	default:
		return 0
	}
}

func decodeJSON(value string) (map[string]any, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var out map[string]any
	if err := decoder.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}
