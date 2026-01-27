package datastore_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/sqliteshim"

	"github.com/sjgoldie/go-restgen/datastore"
)

// TestModel for SQLite tests
type SQLiteTestModel struct {
	bun.BaseModel `bun:"table:test_models"`
	ID            int    `bun:"id,pk,autoincrement"`
	Name          string `bun:"name"`
}

func TestNewSQLite(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		wantErr bool
	}{
		{
			name:    "in-memory database",
			dsn:     ":memory:",
			wantErr: false,
		},
		{
			name:    "file database",
			dsn:     "file::memory:?cache=shared",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := datastore.NewSQLite(tt.dsn)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSQLite() error = %v, wantErr %v", err, tt.wantErr)
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
				// Clean up
				db.Cleanup()
			}
		})
	}
}

func TestSQLite_GetDB(t *testing.T) {
	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		t.Fatal("Failed to create SQLite database:", err)
	}
	defer db.Cleanup()

	bunDB := db.GetDB()
	if bunDB == nil {
		t.Error("Expected non-nil bun.DB from GetDB()")
	}

	// Verify the DB is functional by creating a table
	_, err = bunDB.NewCreateTable().Model((*SQLiteTestModel)(nil)).Exec(context.Background())
	if err != nil {
		t.Error("Expected to be able to create table with returned DB:", err)
	}
}

func TestSQLite_GetTimeout(t *testing.T) {
	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		t.Fatal("Failed to create SQLite database:", err)
	}
	defer db.Cleanup()

	timeout := db.GetTimeout()
	if timeout <= 0 {
		t.Error("Expected positive timeout duration")
	}
}

func TestSQLite_Cleanup(t *testing.T) {
	tests := []struct {
		name string
		test func(t *testing.T)
	}{
		{
			name: "cleanup closes database",
			test: func(t *testing.T) {
				db, err := datastore.NewSQLite(":memory:")
				if err != nil {
					t.Fatal("Failed to create SQLite database:", err)
				}

				// Cleanup should not panic
				db.Cleanup()

				// After cleanup, database operations should fail
				bunDB := db.GetDB()
				_, err = bunDB.NewCreateTable().Model((*SQLiteTestModel)(nil)).Exec(context.Background())
				if err == nil {
					t.Error("Expected error after cleanup, but operation succeeded")
				}
			},
		},
		{
			name: "multiple cleanup calls don't panic",
			test: func(t *testing.T) {
				db, err := datastore.NewSQLite(":memory:")
				if err != nil {
					t.Fatal("Failed to create SQLite database:", err)
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

func TestSQLite_BasicOperations(t *testing.T) {
	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		t.Fatal("Failed to create SQLite database:", err)
	}
	defer db.Cleanup()

	bunDB := db.GetDB()

	// Create table
	_, err = bunDB.NewCreateTable().Model((*SQLiteTestModel)(nil)).IfNotExists().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	// Insert
	model := &SQLiteTestModel{Name: "test"}
	_, err = bunDB.NewInsert().Model(model).Returning("*").Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to insert:", err)
	}
	if model.ID == 0 {
		t.Error("Expected ID to be set after insert")
	}

	// Select
	var retrieved SQLiteTestModel
	err = bunDB.NewSelect().Model(&retrieved).Where("id = ?", model.ID).Scan(context.Background())
	if err != nil {
		t.Fatal("Failed to select:", err)
	}
	if retrieved.Name != "test" {
		t.Errorf("Expected name 'test', got '%s'", retrieved.Name)
	}

	// Update
	retrieved.Name = "updated"
	_, err = bunDB.NewUpdate().Model(&retrieved).WherePK().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to update:", err)
	}

	// Verify update
	var verified SQLiteTestModel
	err = bunDB.NewSelect().Model(&verified).Where("id = ?", model.ID).Scan(context.Background())
	if err != nil {
		t.Fatal("Failed to verify update:", err)
	}
	if verified.Name != "updated" {
		t.Errorf("Expected name 'updated', got '%s'", verified.Name)
	}

	// Delete
	_, err = bunDB.NewDelete().Model(&verified).WherePK().Exec(context.Background())
	if err != nil {
		t.Fatal("Failed to delete:", err)
	}

	// Verify delete
	var notFound SQLiteTestModel
	err = bunDB.NewSelect().Model(&notFound).Where("id = ?", model.ID).Scan(context.Background())
	if err == nil {
		t.Error("Expected error when selecting deleted row")
	}
}

func TestSQLite_StoreInterface(t *testing.T) {
	// Verify SQLite implements Store interface
	var _ datastore.Store = (*datastore.SQLite)(nil)

	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		t.Fatal("Failed to create SQLite database:", err)
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

func TestNewSQLiteWithDB(t *testing.T) {
	// Create external sql.DB
	sqlDB, err := sql.Open(sqliteshim.ShimName, ":memory:")
	if err != nil {
		t.Fatal("Failed to create sql.DB:", err)
	}
	defer func() { _ = sqlDB.Close() }()

	// Create SQLite datastore from existing connection
	db := datastore.NewSQLiteWithDB(sqlDB)

	if db == nil {
		t.Fatal("Expected non-nil SQLite instance")
	}

	// Verify GetDB returns valid bun.DB
	bunDB := db.GetDB()
	if bunDB == nil {
		t.Fatal("Expected non-nil bun.DB from GetDB()")
	}

	// Verify the DB is functional
	_, err = bunDB.NewCreateTable().Model((*SQLiteTestModel)(nil)).Exec(context.Background())
	if err != nil {
		t.Error("Expected to be able to create table:", err)
	}
}

func TestSQLiteWithDB_CleanupDoesNotCloseExternalConnection(t *testing.T) {
	// Create external sql.DB
	sqlDB, err := sql.Open(sqliteshim.ShimName, ":memory:")
	if err != nil {
		t.Fatal("Failed to create sql.DB:", err)
	}
	defer func() { _ = sqlDB.Close() }()

	// Create SQLite datastore from existing connection
	db := datastore.NewSQLiteWithDB(sqlDB)

	// Call Cleanup
	db.Cleanup()

	// External sql.DB should still be usable
	err = sqlDB.Ping()
	if err != nil {
		t.Errorf("External sql.DB should still be usable after Cleanup(), got error: %v", err)
	}
}

func TestSQLiteWithDB_StoreInterface(t *testing.T) {
	sqlDB, err := sql.Open(sqliteshim.ShimName, ":memory:")
	if err != nil {
		t.Fatal("Failed to create sql.DB:", err)
	}
	defer func() { _ = sqlDB.Close() }()

	db := datastore.NewSQLiteWithDB(sqlDB)

	// Use as Store interface
	var store datastore.Store = db

	if store.GetDB() == nil {
		t.Error("Expected non-nil DB from Store interface")
	}

	if store.GetTimeout() <= 0 {
		t.Error("Expected positive timeout from Store interface")
	}

	store.Cleanup()

	// External connection should still work
	if err := sqlDB.Ping(); err != nil {
		t.Errorf("External sql.DB should still be usable after Cleanup(), got error: %v", err)
	}
}
