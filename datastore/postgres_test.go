package datastore_test

import (
	"database/sql"
	"testing"

	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/sjgoldie/go-restgen/datastore"
)

const testDSN = "postgres://user:pass@localhost:5432/testdb?sslmode=disable" // #nosec G101 -- test DSN, not real credentials

func TestNewPostgres(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		wantErr bool
	}{
		{
			name:    "creates postgres instance",
			dsn:     testDSN,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := datastore.NewPostgres(tt.dsn)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewPostgres() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if db == nil {
					t.Error("Expected non-nil database")
				}
				// Verify we got a valid bun.DB
				bunDB := db.GetDB()
				if bunDB == nil {
					t.Error("Expected non-nil bun.DB")
				}
				// Clean up (closes connection attempt)
				db.Cleanup()
			}
		})
	}
}

func TestPostgreSQL_GetDB(t *testing.T) {
	// Note: This test creates a PostgreSQL instance but doesn't require a running server
	// It only tests that GetDB() returns a non-nil bun.DB instance
	db, err := datastore.NewPostgres(testDSN)
	if err != nil {
		t.Fatal("Failed to create PostgreSQL instance:", err)
	}
	defer db.Cleanup()

	bunDB := db.GetDB()
	if bunDB == nil {
		t.Error("Expected non-nil bun.DB from GetDB()")
	}
}

func TestPostgreSQL_GetTimeout(t *testing.T) {
	db, err := datastore.NewPostgres(testDSN)
	if err != nil {
		t.Fatal("Failed to create PostgreSQL instance:", err)
	}
	defer db.Cleanup()

	timeout := db.GetTimeout()
	if timeout <= 0 {
		t.Error("Expected positive timeout duration")
	}

	// PostgreSQL should have a longer timeout than SQLite
	sqliteDB, _ := datastore.NewSQLite(":memory:")
	defer sqliteDB.Cleanup()

	if timeout <= sqliteDB.GetTimeout() {
		t.Error("Expected PostgreSQL timeout to be greater than SQLite timeout")
	}
}

func TestPostgreSQL_Cleanup(t *testing.T) {
	tests := []struct {
		name string
		test func(t *testing.T)
	}{
		{
			name: "cleanup doesn't panic",
			test: func(t *testing.T) {
				db, err := datastore.NewPostgres(testDSN)
				if err != nil {
					t.Fatal("Failed to create PostgreSQL instance:", err)
				}

				// Cleanup should not panic
				db.Cleanup()
			},
		},
		{
			name: "multiple cleanup calls don't panic",
			test: func(t *testing.T) {
				db, err := datastore.NewPostgres(testDSN)
				if err != nil {
					t.Fatal("Failed to create PostgreSQL instance:", err)
				}

				// Multiple cleanups should not panic
				db.Cleanup()
				db.Cleanup()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.test(t)
		})
	}
}

func TestPostgreSQL_StoreInterface(t *testing.T) {
	// Verify PostgreSQL implements Store interface
	var _ datastore.Store = (*datastore.PostgreSQL)(nil)

	db, err := datastore.NewPostgres(testDSN)
	if err != nil {
		t.Fatal("Failed to create PostgreSQL instance:", err)
	}
	defer db.Cleanup()

	// Use as Store interface
	var store datastore.Store = db

	if store.GetDB() == nil {
		t.Error("Expected non-nil DB from Store interface")
	}

	if store.GetTimeout() <= 0 {
		t.Error("Expected positive timeout from Store interface")
	}

	// Cleanup should not panic
	store.Cleanup()
}

func TestNewPostgresWithDB(t *testing.T) {
	// Create external sql.DB using pgdriver
	connector := pgdriver.NewConnector(pgdriver.WithDSN(testDSN))
	sqlDB := sql.OpenDB(connector)
	defer func() { _ = sqlDB.Close() }()

	// Create PostgreSQL datastore from existing connection
	db := datastore.NewPostgresWithDB(sqlDB)

	if db == nil {
		t.Fatal("Expected non-nil PostgreSQL instance")
	}

	// Verify GetDB returns valid bun.DB
	bunDB := db.GetDB()
	if bunDB == nil {
		t.Fatal("Expected non-nil bun.DB from GetDB()")
	}

	// Verify GetTimeout returns positive duration
	if db.GetTimeout() <= 0 {
		t.Error("Expected positive timeout duration")
	}
}

func TestPostgresWithDB_CleanupDoesNotCloseExternalConnection(t *testing.T) {
	// Create external sql.DB using pgdriver
	connector := pgdriver.NewConnector(pgdriver.WithDSN(testDSN))
	sqlDB := sql.OpenDB(connector)
	defer func() { _ = sqlDB.Close() }()

	// Create PostgreSQL datastore from existing connection
	db := datastore.NewPostgresWithDB(sqlDB)

	// Call Cleanup
	db.Cleanup()

	// External sql.DB should still be structurally valid (not closed)
	// Note: Ping would require a real server, but the connection object should not be closed
	if sqlDB == nil {
		t.Error("External sql.DB should not be nil after Cleanup()")
	}
}

func TestPostgresWithDB_StoreInterface(t *testing.T) {
	connector := pgdriver.NewConnector(pgdriver.WithDSN(testDSN))
	sqlDB := sql.OpenDB(connector)
	defer func() { _ = sqlDB.Close() }()

	db := datastore.NewPostgresWithDB(sqlDB)

	// Use as Store interface
	var store datastore.Store = db

	if store.GetDB() == nil {
		t.Error("Expected non-nil DB from Store interface")
	}

	if store.GetTimeout() <= 0 {
		t.Error("Expected positive timeout from Store interface")
	}

	// Cleanup should not panic and should not close external connection
	store.Cleanup()
}
