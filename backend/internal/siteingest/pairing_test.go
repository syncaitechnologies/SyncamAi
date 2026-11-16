package siteingest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

func pemPublicKey(t *testing.T, key any) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

// boxPublicKeyPEM returns a fresh P-256 public key in PEM form.
func boxPublicKeyPEM(t *testing.T) string {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pemPublicKey(t, &private.PublicKey)
}

func TestPairingCodeFormatAndPublicKeyRules(t *testing.T) {
	for input, want := range map[string]string{"k7q4-xm92": "K7Q4XM92", " K7Q4 XM92 ": "K7Q4XM92", "K7Q4XM92": "K7Q4XM92"} {
		if got := NormalizePairingCode(input); got != want {
			t.Fatalf("normalize %q = %q", input, got)
		}
	}
	if FormatPairingCode("K7Q4XM92") != "K7Q4-XM92" || FormatPairingCode("short") != "short" {
		t.Fatal("format")
	}
	if err := ValidateBoxPublicKey(""); err != nil {
		t.Fatalf("empty key is allowed for older boxes: %v", err)
	}
	if err := ValidateBoxPublicKey(boxPublicKeyPEM(t)); err != nil {
		t.Fatalf("P-256 key: %v", err)
	}
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 1024)
	for name, bad := range map[string]string{
		"not pem": "hello", "wrong curve": pemPublicKey(t, &p384.PublicKey), "rsa": pemPublicKey(t, &rsaKey.PublicKey),
		"wrong block": string(pem.EncodeToMemory(&pem.Block{Type: "EC PARAMETERS", Bytes: []byte{1, 2, 3}})),
	} {
		if err := ValidateBoxPublicKey(bad); !errors.Is(err, ErrPublicKeyInvalid) {
			t.Fatalf("%s must be rejected: %v", name, err)
		}
	}
}

func TestMemoryPairingIsSingleUseAndExpires(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryKeyStore(testTenant + ":" + testSite)
	if _, _, err := store.CreatePairingCode(ctx, testTenant, "unknown-site", "admin"); !errors.Is(err, ErrSiteNotFound) {
		t.Fatalf("unknown site: %v", err)
	}
	code, expires, err := store.CreatePairingCode(ctx, testTenant, testSite, "admin")
	if err != nil || len(code) != 8 || time.Until(expires) < 29*time.Minute {
		t.Fatalf("create: %v %q %v", err, code, expires)
	}
	publicKey := boxPublicKeyPEM(t)
	raw, scope, err := store.RedeemPairingCode(ctx, FormatPairingCode(code), publicKey, "Front office PC")
	if err != nil || scope.SiteID != testSite {
		t.Fatalf("redeem: %v %+v", err, scope)
	}
	if resolved, err := store.Resolve(ctx, raw); err != nil || resolved.KeyID != scope.KeyID {
		t.Fatalf("the paired key must work: %v", err)
	}
	keys, _ := store.List(ctx, testTenant, testSite)
	if len(keys) != 1 || keys[0].Label != "Front office PC" || keys[0].BoxPublicKey != strings.TrimSpace(publicKey) {
		t.Fatalf("paired key metadata: %+v", keys)
	}
	if _, _, err := store.RedeemPairingCode(ctx, code, "", ""); !errors.Is(err, ErrPairingInvalid) {
		t.Fatalf("a code works once: %v", err)
	}
	expired, _, _ := store.CreatePairingCode(ctx, testTenant, testSite, "admin")
	store.ExpirePairingCode(expired)
	if _, _, err := store.RedeemPairingCode(ctx, expired, "", ""); !errors.Is(err, ErrPairingInvalid) {
		t.Fatalf("an expired code fails: %v", err)
	}
	if _, _, err := store.RedeemPairingCode(ctx, "NOPE1234", "", ""); !errors.Is(err, ErrPairingInvalid) {
		t.Fatalf("an unknown code fails: %v", err)
	}
	fresh, _, _ := store.CreatePairingCode(ctx, testTenant, testSite, "admin")
	if _, _, err := store.RedeemPairingCode(ctx, fresh, "garbage", ""); !errors.Is(err, ErrPublicKeyInvalid) {
		t.Fatalf("a bad public key fails before the code is used: %v", err)
	}
	if _, _, err := store.RedeemPairingCode(ctx, fresh, "", ""); err != nil {
		t.Fatalf("the code is still usable after a rejected key: %v", err)
	}
}

func TestPostgresPairing(t *testing.T) {
	mock := newMock(t)
	store := NewPostgresKeyStore(mock)
	ctx := context.Background()

	expectTenantTx(mock, pgx.ReadWrite)
	mock.ExpectQuery("SELECT EXISTS").WithArgs(testTenant, testSite).WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectExec("INSERT INTO platform.pairing_codes").WithArgs(pgxmock.AnyArg(), testTenant, testSite, "admin", pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	code, _, err := store.CreatePairingCode(ctx, testTenant, testSite, "admin")
	if err != nil || len(code) != 8 {
		t.Fatalf("create: %v %q", err, code)
	}

	expectTenantTx(mock, pgx.ReadWrite)
	mock.ExpectQuery("SELECT EXISTS").WithArgs(testTenant, testSite).WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()
	if _, _, err := store.CreatePairingCode(ctx, testTenant, testSite, "admin"); !errors.Is(err, ErrSiteNotFound) {
		t.Fatalf("retired site: %v", err)
	}

	if _, _, err := store.RedeemPairingCode(ctx, "short", "", ""); !errors.Is(err, ErrPairingInvalid) {
		t.Fatalf("malformed codes never reach the database: %v", err)
	}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectQuery("redeem_pairing_code").WithArgs(HashKey("K7Q4XM92"), pgxmock.AnyArg(), pgxmock.AnyArg(), "Office PC", "").
		WillReturnRows(pgxmock.NewRows([]string{"tenant_id", "site_id"}).AddRow(testTenant, testSite))
	mock.ExpectCommit()
	mock.ExpectRollback()
	raw, scope, err := store.RedeemPairingCode(ctx, "k7q4-xm92", "", " Office PC ")
	if err != nil || !validRawKey(raw) || scope.TenantID != testTenant || scope.SiteID != testSite || scope.KeyID == "" {
		t.Fatalf("redeem: %v %+v", err, scope)
	}

	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectQuery("redeem_pairing_code").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnRows(pgxmock.NewRows([]string{"tenant_id", "site_id"}))
	mock.ExpectRollback()
	if _, _, err := store.RedeemPairingCode(ctx, "K7Q4XM92", "", ""); !errors.Is(err, ErrPairingInvalid) {
		t.Fatalf("used or expired code: %v", err)
	}
	failure := errors.New("database down")
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectQuery("redeem_pairing_code").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnError(failure)
	mock.ExpectRollback()
	if _, _, err := store.RedeemPairingCode(ctx, "K7Q4XM92", "", ""); !errors.Is(err, failure) {
		t.Fatalf("database failure: %v", err)
	}
}
