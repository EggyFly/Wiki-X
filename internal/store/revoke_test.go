package store

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/EggyFly/Wiki-X/internal/revoke"
)

func TestBusinessDeletesKeepRevokedRows(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	admin, err := s.CreateUser(ctx, "revoke-admin", "revoke-admin@example.com", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateGroup(ctx, "revoke-group", []string{"read"}, admin.ID); err != nil {
		t.Fatal(err)
	}
	var groupID int64
	if err := s.pool.QueryRow(ctx, `SELECT id FROM groups WHERE name='revoke-group'`).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGroup(ctx, groupID, admin.ID); err != nil {
		t.Fatal(err)
	}
	assertRevoked(t, s, `SELECT revoked_at IS NOT NULL FROM groups WHERE id=$1`, groupID)

	entityType, err := s.CreateEntityType(ctx, "revoke-type", "Revoke", admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddEntityField(ctx, entityType.ID, "level", "Level", "number", false, nil, 1, admin.ID); err != nil {
		t.Fatal(err)
	}
	var fieldID int64
	if err := s.pool.QueryRow(ctx, `SELECT id FROM entity_fields WHERE type_id=$1 AND name='level'`, entityType.ID).Scan(&fieldID); err != nil {
		t.Fatal(err)
	}
	entity, err := s.SaveEntity(ctx, EntityInput{TypeID: entityType.ID, Name: "revoke-entity", Label: "Revoke", Data: map[string]string{"level": "1"}, ActorID: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteEntityField(ctx, fieldID, admin.ID); err != nil {
		t.Fatal(err)
	}
	assertRevoked(t, s, `SELECT revoked_at IS NOT NULL FROM entity_fields WHERE id=$1`, fieldID)
	if err := s.RestoreRevokedRecord(ctx, "entity_field", strconv.FormatInt(fieldID, 10), admin.ID); err != nil {
		t.Fatal(err)
	}
	assertActive(t, s, `SELECT revoked_at IS NULL FROM entity_fields WHERE id=$1`, fieldID)
	if err := s.DeleteEntity(ctx, entity.ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	assertRevoked(t, s, `SELECT revoked_at IS NOT NULL FROM entities WHERE id=$1`, entity.ID)
	if err := s.DeleteEntityType(ctx, entityType.ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	assertRevoked(t, s, `SELECT revoked_at IS NOT NULL FROM entity_types WHERE id=$1`, entityType.ID)

	if err := s.CreateInvite(ctx, "revoke-invite-hash", "Revoke", 1, nil, admin.ID); err != nil {
		t.Fatal(err)
	}
	var inviteID int64
	if err := s.pool.QueryRow(ctx, `SELECT id FROM invite_codes WHERE code_hash='revoke-invite-hash'`).Scan(&inviteID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteInvite(ctx, inviteID, admin.ID); err != nil {
		t.Fatal(err)
	}
	assertRevoked(t, s, `SELECT revoked_at IS NOT NULL FROM invite_codes WHERE id=$1`, inviteID)

	client := OIDCClient{
		ClientID: "revoke-client", SecretHash: "hash", Name: "Revoke",
		RedirectURIs: []string{"https://app.example.com/callback"}, Scopes: "openid",
		Enabled: true, AuthMethod: "client_secret_basic",
	}
	if err := s.CreateOIDCClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteOIDCClient(ctx, client.ClientID, admin.ID); err != nil {
		t.Fatal(err)
	}
	assertRevoked(t, s, `SELECT revoked_at IS NOT NULL FROM oidc_clients WHERE client_id=$1`, client.ClientID)

	if err := s.Watch(ctx, admin.ID, "/guide/"); err != nil {
		t.Fatal(err)
	}
	if err := s.Unwatch(ctx, admin.ID, "/guide/"); err != nil {
		t.Fatal(err)
	}
	assertRevoked(t, s, `SELECT revoked_at IS NOT NULL FROM watchlist WHERE user_id=$1 AND route=$2`, admin.ID, "/guide/")

	records, err := s.ListRevokedRecords(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) < 5 {
		t.Fatalf("revoked records = %d", len(records))
	}
	if err := s.RestoreRevokedRecord(ctx, "group", strconv.FormatInt(groupID, 10), admin.ID); err != nil {
		t.Fatal(err)
	}
	assertActive(t, s, `SELECT revoked_at IS NULL FROM groups WHERE id=$1`, groupID)
	if err := s.RestoreRevokedRecord(ctx, "invite", strconv.FormatInt(inviteID, 10), admin.ID); err != nil {
		t.Fatal(err)
	}
	assertActive(t, s, `SELECT revoked_at IS NULL FROM invite_codes WHERE id=$1`, inviteID)
	if err := s.RestoreRevokedRecord(ctx, "oidc_client", client.ClientID, admin.ID); err != nil {
		t.Fatal(err)
	}
	assertActive(t, s, `SELECT revoked_at IS NULL AND enabled=false FROM oidc_clients WHERE client_id=$1`, client.ClientID)

	user, err := s.CreateUser(ctx, "revoke-user", "revoke-user@example.com", "hash", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserGroups(ctx, user.ID, []string{"editor"}, admin.ID); err != nil {
		t.Fatal(err)
	}
	assertRevoked(t, s, `SELECT revoked_at IS NOT NULL FROM user_groups
		WHERE user_id=$1 AND group_id=(SELECT id FROM groups WHERE name='user')`, user.ID)
	if err := s.SetUserGroups(ctx, user.ID, []string{"user"}, admin.ID); err != nil {
		t.Fatal(err)
	}
	var active bool
	if err := s.pool.QueryRow(ctx, `SELECT revoked_at IS NULL FROM user_groups
		WHERE user_id=$1 AND group_id=(SELECT id FROM groups WHERE name='user')`, user.ID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if !active {
		t.Fatal("revoked membership was not restored")
	}
}

func assertRevoked(t *testing.T, s *Store, query string, args ...any) {
	t.Helper()
	var revoked bool
	if err := s.pool.QueryRow(context.Background(), query, args...).Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	if !revoked {
		t.Fatal("record was not revoked")
	}
}

func assertActive(t *testing.T, s *Store, query string, args ...any) {
	t.Helper()
	var active bool
	if err := s.pool.QueryRow(context.Background(), query, args...).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if !active {
		t.Fatal("record was not restored")
	}
}

func TestRevokePolicyDisablesDelete(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	admin, err := s.CreateUser(ctx, "policy-admin", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateGroup(ctx, "policy-group", []string{"read"}, admin.ID); err != nil {
		t.Fatal(err)
	}
	var groupID int64
	if err := s.pool.QueryRow(ctx, `SELECT id FROM groups WHERE name='policy-group'`).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	s.SetRevokePolicy(revoke.Policy{Sections: map[string]bool{revoke.SectionGroup: false}})
	if err := s.DeleteGroup(ctx, groupID, admin.ID); !errors.Is(err, ErrRevokeDisabled) {
		t.Fatalf("expected disabled delete, got %v", err)
	}
	var revoked bool
	if err := s.pool.QueryRow(ctx, `SELECT revoked_at IS NOT NULL FROM groups WHERE id=$1`, groupID).Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	if revoked {
		t.Fatal("disabled delete must not revoke the group")
	}
	s.SetRevokePolicy(revoke.Policy{})
	if err := s.DeleteGroup(ctx, groupID, admin.ID); err != nil {
		t.Fatal(err)
	}
}
