package router

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/sjgoldie/go-restgen/datastore"
)

// switchableStore is test-only Store infrastructure that wraps a real SQLite
// store by default but can be temporarily swapped to a sqlmock-backed bun.DB
// for tests that need to verify exact SQL and transaction lifecycle.
//
// This exists because the datastore singleton is locked by sync.Once, so
// individual tests can't replace it. The switchable wrapper is registered
// once in TestMain; tests opt into mock mode via useSqlmock(t) which sets
// the mock backend for the test's duration and restores SQLite via t.Cleanup.
type switchableStore struct {
	mu     sync.Mutex
	sqlite datastore.Store
	mockDB *bun.DB
}

func (s *switchableStore) GetDB() *bun.DB {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mockDB != nil {
		return s.mockDB
	}
	return s.sqlite.GetDB()
}

func (s *switchableStore) GetTimeout() time.Duration { return 30 * time.Second }

func (s *switchableStore) IlikeOp() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mockDB != nil {
		// Mock backend uses pgdialect, so report PostgreSQL's case-insensitive operator
		return "ILIKE"
	}
	return s.sqlite.IlikeOp()
}

func (s *switchableStore) Cleanup() {
	if s.sqlite != nil {
		s.sqlite.Cleanup()
	}
}

// testStore is the package-level switchable store registered in TestMain.
var testStore *switchableStore

// useSqlmock installs a sqlmock-backed bun.DB as the active backend for the
// duration of the test, returning the mock controller. Call mock.ExpectXxx()
// to script expected queries. The mock and any expectations are cleaned up
// automatically when the test completes.
func useSqlmock(t *testing.T) sqlmock.Sqlmock {
	t.Helper()

	// Regexp matcher: bun's query layer inlines ? placeholders into the SQL
	// string before reaching the driver, so tests assert against the rendered
	// SQL via regex rather than parameter binding.
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	bunDB := bun.NewDB(sqlDB, pgdialect.New())

	testStore.mu.Lock()
	testStore.mockDB = bunDB
	testStore.mu.Unlock()

	t.Cleanup(func() {
		testStore.mu.Lock()
		testStore.mockDB = nil
		testStore.mu.Unlock()
		_ = bunDB.Close()
		_ = sqlDB.Close()
	})

	return mock
}

func TestMain(m *testing.M) {
	sqlite, err := datastore.NewSQLite(":memory:")
	if err != nil {
		panic("failed to create test sqlite database: " + err.Error())
	}

	testStore = &switchableStore{sqlite: sqlite}

	if err := datastore.Initialize(testStore); err != nil {
		sqlite.Cleanup()
		panic("failed to initialize datastore: " + err.Error())
	}

	code := m.Run()

	datastore.Cleanup()

	os.Exit(code)
}
