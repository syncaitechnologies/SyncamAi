package retention

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/siteingest"
)

const tenant = "11111111-1111-4111-8111-111111111111"

type fakeStore struct {
	mu       sync.Mutex
	batches  [][]ExpiredAlert
	purged   [][]string
	listErr  error
	purgeErr error
}

func (s *fakeStore) Expired(_ context.Context, limit int) ([]ExpiredAlert, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listErr != nil {
		return nil, s.listErr
	}
	if len(s.batches) == 0 {
		return nil, nil
	}
	batch := s.batches[0]
	s.batches = s.batches[1:]
	if len(batch) > limit {
		batch = batch[:limit]
	}
	return batch, nil
}

func (s *fakeStore) Purge(_ context.Context, ids []string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purged = append(s.purged, ids)
	return len(ids), s.purgeErr
}

type fakeEvidence struct {
	deleted []string
	fail    map[string]error
}

func (e *fakeEvidence) Delete(_ context.Context, tenantID, ref string) error {
	if err := e.fail[ref]; err != nil {
		return err
	}
	e.deleted = append(e.deleted, tenantID+"|"+ref)
	return nil
}

func expired(id string, refs ...string) ExpiredAlert {
	return ExpiredAlert{AlertID: id, TenantID: tenant, EvidenceRefs: refs}
}

func TestRunOnceDeletesSnapshotsThenAlertsInBatches(t *testing.T) {
	store := &fakeStore{batches: [][]ExpiredAlert{
		{expired("a1", "snapshot:"+tenant+"/a.jpg", "track:7"), expired("a2")},
		{expired("a3", "snapshot:"+tenant+"/gone.jpg")},
	}}
	evidence := &fakeEvidence{fail: map[string]error{"snapshot:" + tenant + "/gone.jpg": siteingest.ErrEvidenceNotFound}}
	purger := &Purger{Store: store, Evidence: evidence, BatchSize: 2}
	removed, err := purger.RunOnce(context.Background())
	if err != nil || removed != 3 {
		t.Fatalf("removed %d, err %v", removed, err)
	}
	if len(evidence.deleted) != 1 || evidence.deleted[0] != tenant+"|snapshot:"+tenant+"/a.jpg" {
		t.Fatalf("only snapshot references are deleted, in their own workspace: %v", evidence.deleted)
	}
	if len(store.purged) != 2 || strings.Join(store.purged[0], ",") != "a1,a2" || strings.Join(store.purged[1], ",") != "a3" {
		t.Fatalf("a snapshot that is already gone does not block its alert: %v", store.purged)
	}
}

func TestRunOnceKeepsAlertsWhoseSnapshotsCouldNotBeDeleted(t *testing.T) {
	var logged []string
	store := &fakeStore{batches: [][]ExpiredAlert{
		{expired("a1", "snapshot:"+tenant+"/a.jpg"), expired("a2", "snapshot:"+tenant+"/b.jpg")},
		{expired("a3")},
	}}
	evidence := &fakeEvidence{fail: map[string]error{"snapshot:" + tenant + "/a.jpg": errors.New("storage down")}}
	purger := &Purger{Store: store, Evidence: evidence, BatchSize: 2, Logf: func(f string, _ ...any) { logged = append(logged, f) }}
	removed, err := purger.RunOnce(context.Background())
	if err != nil || removed != 1 || len(store.purged) != 1 || store.purged[0][0] != "a2" {
		t.Fatalf("a failed snapshot keeps its alert and ends the run: %d %v %v", removed, err, store.purged)
	}
	if len(logged) != 1 {
		t.Fatalf("the kept alert is logged: %v", logged)
	}

	allFail := &fakeStore{batches: [][]ExpiredAlert{{expired("a1", "snapshot:"+tenant+"/a.jpg")}}}
	purger = &Purger{Store: allFail, Evidence: evidence}
	if _, err := purger.RunOnce(context.Background()); err == nil || len(allFail.purged) != 0 {
		t.Fatalf("a batch with no deletable snapshots purges nothing: %v %v", err, allFail.purged)
	}
}

func TestRunOnceWithoutSnapshotStorageAndOnFailures(t *testing.T) {
	store := &fakeStore{batches: [][]ExpiredAlert{{expired("a1", "snapshot:"+tenant+"/a.jpg")}}}
	if removed, err := (&Purger{Store: store}).RunOnce(context.Background()); err != nil || removed != 1 {
		t.Fatalf("without storage configured the records are still deleted: %d %v", removed, err)
	}
	if _, err := (&Purger{Store: &fakeStore{listErr: errors.New("db down")}}).RunOnce(context.Background()); err == nil {
		t.Fatal("a list failure is reported")
	}
	failing := &fakeStore{batches: [][]ExpiredAlert{{expired("a1")}}, purgeErr: errors.New("db down")}
	if _, err := (&Purger{Store: failing}).RunOnce(context.Background()); err == nil {
		t.Fatal("a purge failure is reported")
	}
	if _, err := (*Purger)(nil).RunOnce(context.Background()); err == nil {
		t.Fatal("a missing purger fails")
	}
	full := &fakeStore{}
	for i := 0; i < maxBatchesPerRun+5; i++ {
		full.batches = append(full.batches, []ExpiredAlert{expired("x")})
	}
	if removed, _ := (&Purger{Store: full, BatchSize: 1}).RunOnce(context.Background()); removed != maxBatchesPerRun {
		t.Fatalf("one run is bounded: %d", removed)
	}
}

func TestRunPurgesAfterTheFirstDelayAndStopsWithContext(t *testing.T) {
	var mu sync.Mutex
	var logged []string
	store := &fakeStore{batches: [][]ExpiredAlert{{expired("a1")}}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	purger := &Purger{Store: store, Logf: func(f string, _ ...any) {
		mu.Lock()
		logged = append(logged, f)
		mu.Unlock()
		cancel()
	}}
	go func() {
		purger.Run(ctx, time.Millisecond, time.Hour)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not stop")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(logged) != 1 || !strings.Contains(logged[0], "deleted") {
		t.Fatalf("a run logs what it deleted: %v", logged)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	(&Purger{Store: store}).Run(cancelled, time.Hour, time.Hour)
	failingCtx, cancelFailing := context.WithCancel(context.Background())
	failing := &Purger{Store: &fakeStore{listErr: errors.New("db down")}, Logf: func(f string, _ ...any) {
		if strings.Contains(f, "before the error") {
			cancelFailing()
		}
	}}
	failing.Run(failingCtx, time.Millisecond, time.Hour)
}

func TestPostgresStoreCallsTheRetentionFunctions(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectQuery("alerts.expired_alert_evidence").WithArgs(200).WillReturnRows(
		pgxmock.NewRows([]string{"alert_id", "tenant_id", "evidence_refs"}).AddRow("a1", tenant, []string{"snapshot:" + tenant + "/a.jpg"}))
	mock.ExpectCommit()
	mock.ExpectRollback()
	got, err := NewPostgresStore(mock).Expired(context.Background(), 200)
	if err != nil || len(got) != 1 || got[0].AlertID != "a1" || got[0].EvidenceRefs[0] != "snapshot:"+tenant+"/a.jpg" {
		t.Fatalf("expired: %+v %v", got, err)
	}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectQuery("alerts.purge_expired_alerts").WithArgs([]string{"a1"}).WillReturnRows(pgxmock.NewRows([]string{"removed"}).AddRow(1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	if removed, err := NewPostgresStore(mock).Purge(context.Background(), []string{"a1"}); err != nil || removed != 1 {
		t.Fatalf("purge: %d %v", removed, err)
	}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	mock.ExpectQuery("alerts.expired_alert_evidence").WithArgs(1).WillReturnError(errors.New("denied"))
	mock.ExpectRollback()
	if _, err := NewPostgresStore(mock).Expired(context.Background(), 1); err == nil {
		t.Fatal("query failure surfaces")
	}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite})
	mock.ExpectQuery("alerts.purge_expired_alerts").WithArgs([]string{"a1"}).WillReturnError(errors.New("denied"))
	mock.ExpectRollback()
	if _, err := NewPostgresStore(mock).Purge(context.Background(), []string{"a1"}); err == nil {
		t.Fatal("purge failure surfaces")
	}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly}).WillReturnError(errors.New("pool closed"))
	if _, err := NewPostgresStore(mock).Expired(context.Background(), 1); err == nil {
		t.Fatal("begin failure surfaces")
	}
	mock.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadWrite}).WillReturnError(errors.New("pool closed"))
	if _, err := NewPostgresStore(mock).Purge(context.Background(), []string{"a1"}); err == nil {
		t.Fatal("begin failure surfaces")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if _, err := (*PostgresStore)(nil).Expired(context.Background(), 1); err == nil {
		t.Fatal("missing store fails")
	}
	if _, err := (*PostgresStore)(nil).Purge(context.Background(), nil); err == nil {
		t.Fatal("missing store fails")
	}
}
