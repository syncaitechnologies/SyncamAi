package siteingest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

const testKeyID = "33333333-3333-4333-8333-333333333333"

func newMock(t *testing.T) pgxmock.PgxPoolIface {
	t.Helper()
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
		mock.Close()
	})
	return mock
}

func expectTenantTx(mock pgxmock.PgxPoolIface, mode pgx.TxAccessMode) {
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: mode})
	mock.ExpectExec("SELECT set_config").WithArgs(testTenant).WillReturnResult(pgxmock.NewResult("SELECT", 1))
}

func TestPostgresKeyStoreResolveCreateListRevokeHeartbeat(t *testing.T) {
	mock := newMock(t)
	store := NewPostgresKeyStore(mock)
	ctx := context.Background()
	raw, _ := NewRawKey()

	if _, err := store.Resolve(ctx, "bad"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("malformed key must not query: %v", err)
	}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectQuery("resolve_site_key").WithArgs(HashKey(raw)).WillReturnRows(pgxmock.NewRows([]string{"key_id", "tenant_id", "site_id"}).AddRow(testKeyID, testTenant, testSite))
	mock.ExpectCommit()
	scope, err := store.Resolve(ctx, raw)
	if err != nil || scope.KeyID != testKeyID || scope.SiteID != testSite {
		t.Fatalf("resolve: %v %+v", err, scope)
	}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectQuery("resolve_site_key").WithArgs(HashKey(raw)).WillReturnRows(pgxmock.NewRows([]string{"key_id", "tenant_id", "site_id"}))
	mock.ExpectRollback()
	if _, err := store.Resolve(ctx, raw); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("unknown key: %v", err)
	}

	expectTenantTx(mock, pgx.ReadWrite)
	mock.ExpectQuery("SELECT EXISTS").WithArgs(testTenant, testSite).WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("INSERT INTO platform.site_keys").WithArgs(pgxmock.AnyArg(), testTenant, testSite, "Gate box", pgxmock.AnyArg(), "admin").WillReturnRows(pgxmock.NewRows([]string{"created_at"}).AddRow(time.Now()))
	mock.ExpectCommit()
	created, key, err := store.Create(ctx, testTenant, testSite, "admin", " Gate box ")
	if err != nil || !validRawKey(created) || key.Label != "Gate box" {
		t.Fatalf("create: %v %+v", err, key)
	}
	expectTenantTx(mock, pgx.ReadWrite)
	mock.ExpectQuery("SELECT EXISTS").WithArgs(testTenant, testSite).WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()
	if _, _, err := store.Create(ctx, testTenant, testSite, "admin", "x"); !errors.Is(err, ErrSiteNotFound) {
		t.Fatalf("missing site: %v", err)
	}

	seen := time.Now()
	expectTenantTx(mock, pgx.ReadOnly)
	mock.ExpectQuery("FROM platform.site_keys").WithArgs(testSite).WillReturnRows(pgxmock.NewRows([]string{"id", "tenant_id", "site_id", "label", "created_at", "revoked_at", "last_seen_at", "camera_status"}).
		AddRow(testKeyID, testTenant, testSite, "Box", seen, nil, &seen, []byte(`{"c":{"online":true}}`)))
	mock.ExpectCommit()
	keys, err := store.List(ctx, testTenant, testSite)
	if err != nil || len(keys) != 1 || !keys[0].CameraStatus["c"].Online || keys[0].LastSeenAt == nil {
		t.Fatalf("list: %v %+v", err, keys)
	}

	if err := store.Revoke(ctx, testTenant, "not-a-uuid"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("bad id: %v", err)
	}
	expectTenantTx(mock, pgx.ReadWrite)
	mock.ExpectExec("UPDATE platform.site_keys SET revoked_at").WithArgs(testKeyID).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectCommit()
	if err := store.Revoke(ctx, testTenant, testKeyID); err != nil {
		t.Fatal(err)
	}
	expectTenantTx(mock, pgx.ReadWrite)
	mock.ExpectExec("UPDATE platform.site_keys SET revoked_at").WithArgs(testKeyID).WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	mock.ExpectRollback()
	if err := store.Revoke(ctx, testTenant, testKeyID); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("missing revoke: %v", err)
	}

	expectTenantTx(mock, pgx.ReadWrite)
	mock.ExpectExec("UPDATE platform.site_keys SET last_seen_at").WithArgs(testKeyID, pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectCommit()
	if err := store.RecordHeartbeat(ctx, Scope{KeyID: testKeyID, TenantID: testTenant, SiteID: testSite}, map[string]CameraStatus{"c": {Online: true}}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresKeyStoreSurfacesStorageFailures(t *testing.T) {
	mock := newMock(t)
	store := NewPostgresKeyStore(mock)
	ctx := context.Background()
	failure := errors.New("database down")
	raw, _ := NewRawKey()
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly}).WillReturnError(failure)
	if _, err := store.Resolve(ctx, raw); !errors.Is(err, failure) {
		t.Fatalf("begin failure: %v", err)
	}
	if _, err := store.List(ctx, "not-a-tenant", ""); err == nil {
		t.Fatal("invalid tenant must fail")
	}
	expectTenantTx(mock, pgx.ReadOnly)
	mock.ExpectQuery("FROM platform.site_keys").WithArgs(nil).WillReturnError(failure)
	mock.ExpectRollback()
	if _, err := store.List(ctx, testTenant, ""); !errors.Is(err, failure) {
		t.Fatalf("list failure: %v", err)
	}
	expectTenantTx(mock, pgx.ReadWrite)
	mock.ExpectExec("UPDATE platform.site_keys SET last_seen_at").WithArgs(testKeyID, pgxmock.AnyArg()).WillReturnError(failure)
	mock.ExpectRollback()
	if err := store.RecordHeartbeat(ctx, Scope{KeyID: testKeyID, TenantID: testTenant}, nil); !errors.Is(err, failure) {
		t.Fatalf("heartbeat failure: %v", err)
	}
}

func TestPostgresTargetStoreLinksListsAndDeletes(t *testing.T) {
	mock := newMock(t)
	store := NewPostgresTargetStore(mock)
	ctx := context.Background()

	expectTenantTx(mock, pgx.ReadWrite)
	mock.ExpectQuery("SELECT EXISTS").WithArgs(testTenant, testSite).WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectExec("INSERT INTO notify.telegram_link_codes").WithArgs(pgxmock.AnyArg(), testTenant, testSite, "admin", pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	code, expiresAt, err := store.CreateLinkCode(ctx, testTenant, testSite, "admin")
	if err != nil || len(code) != 8 || time.Until(expiresAt) > LinkCodeTTL {
		t.Fatalf("link code: %v %q", err, code)
	}
	expectTenantTx(mock, pgx.ReadWrite)
	mock.ExpectQuery("SELECT EXISTS").WithArgs(testTenant, testSite).WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()
	if _, _, err := store.CreateLinkCode(ctx, testTenant, testSite, "admin"); !errors.Is(err, ErrSiteNotFound) {
		t.Fatalf("missing site: %v", err)
	}

	if _, _, ok, err := store.Redeem(ctx, "short", 1, "x"); ok || err != nil {
		t.Fatal("malformed code must not query")
	}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectQuery("redeem_telegram_link").WithArgs(HashKey("ABCDEFGH"), int64(9), "Team").WillReturnRows(pgxmock.NewRows([]string{"tenant_id", "site_id"}).AddRow(testTenant, testSite))
	mock.ExpectCommit()
	tenantID, siteID, ok, err := store.Redeem(ctx, " abcdefgh ", 9, "Team")
	if err != nil || !ok || tenantID != testTenant || siteID != testSite {
		t.Fatalf("redeem: %v %v", ok, err)
	}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectQuery("redeem_telegram_link").WithArgs(HashKey("ABCDEFGH"), int64(9), "Team").WillReturnRows(pgxmock.NewRows([]string{"tenant_id", "site_id"}))
	mock.ExpectRollback()
	if _, _, ok, err := store.Redeem(ctx, "ABCDEFGH", 9, "Team"); ok || err != nil {
		t.Fatalf("used code: %v %v", ok, err)
	}

	expectTenantTx(mock, pgx.ReadOnly)
	mock.ExpectQuery("FROM notify.telegram_targets").WithArgs(testSite).WillReturnRows(pgxmock.NewRows([]string{"id", "tenant_id", "site_id", "chat_id", "chat_title", "min_severity", "muted", "created_at"}).
		AddRow(testKeyID, testTenant, testSite, int64(9), "Team", "medium", false, time.Now()))
	mock.ExpectCommit()
	targets, err := store.Targets(ctx, testTenant, testSite)
	if err != nil || len(targets) != 1 || targets[0].ChatID != 9 {
		t.Fatalf("targets: %v %+v", err, targets)
	}

	expectTenantTx(mock, pgx.ReadWrite)
	mock.ExpectExec("DELETE FROM notify.telegram_targets").WithArgs(testKeyID).WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectCommit()
	if err := store.Delete(ctx, testTenant, testKeyID); err != nil {
		t.Fatal(err)
	}
	expectTenantTx(mock, pgx.ReadWrite)
	mock.ExpectExec("DELETE FROM notify.telegram_targets").WithArgs(testKeyID).WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectRollback()
	if err := store.Delete(ctx, testTenant, testKeyID); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("missing delete: %v", err)
	}
}

func TestMemoryTargetStoreDeleteAndDescribers(t *testing.T) {
	store := NewMemoryTargetStore(TelegramTarget{ID: "a", TenantID: testTenant, SiteID: testSite})
	if err := store.Delete(context.Background(), "other", "a"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("cross-tenant delete: %v", err)
	}
	if err := store.Delete(context.Background(), testTenant, "a"); err != nil {
		t.Fatal(err)
	}
	if name := SiteNamer(nil)(context.Background(), testTenant, testSite); name != "" {
		t.Fatalf("nil repository name: %q", name)
	}
}
