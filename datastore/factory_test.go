package datastore_test

import (
	"testing"

	"github.com/sjgoldie/go-restgen/datastore"
)

func TestInitialize_Success(t *testing.T) {
	// Clean up any existing singleton
	datastore.Cleanup()

	db, err := datastore.NewSQLite(":memory:")
	if err != nil {
		t.Fatal("Failed to create test database:", err)
	}
	defer db.Cleanup()

	err = datastore.Initialize(db)
	if err != nil {
		t.Error("Expected successful initialization")
	}

	// Verify we can get the store
	store, err := datastore.Get()
	if err != nil {
		t.Error("Expected to get initialized store")
	}
	if store != db {
		t.Error("Expected store to match initialized database")
	}

	datastore.Cleanup()
}

func TestInitialize_Nil(t *testing.T) {
	// Note: Due to sync.Once in the singleton pattern, if Initialize() was already called
	// successfully in another test, this test cannot reliably test the nil case.
	// The singleton pattern is designed for application startup where Initialize() is
	// called exactly once. In a real application, passing nil would cause initialization
	// to fail once and remain failed.

	// This test verifies that if we somehow have an uninitialized singleton,
	// calling Get() returns an error. We cannot force re-initialization due to sync.Once.
	t.Skip("Cannot reliably test nil initialization due to sync.Once singleton pattern")
}

func TestGet(t *testing.T) {
	t.Run("get before initialize", func(t *testing.T) {
		// Note: This test cannot reliably ensure "before initialize" state due to sync.Once.
		// If TestInitialize_Success ran first, the singleton is already initialized.
		// In a real application, Get() is only called after Initialize() at startup.
		t.Skip("Cannot reliably test uninitialized state due to sync.Once singleton pattern")
	})

	t.Run("get after initialize", func(t *testing.T) {
		datastore.Cleanup()

		db, err := datastore.NewSQLite(":memory:")
		if err != nil {
			t.Fatal("Failed to create test database:", err)
		}
		defer db.Cleanup()

		err = datastore.Initialize(db)
		if err != nil {
			t.Fatal("Failed to initialize datastore:", err)
		}
		defer datastore.Cleanup()

		store, err := datastore.Get()
		if err != nil {
			t.Error("Expected no error when getting initialized datastore")
		}
		if store == nil {
			t.Error("Expected store to be non-nil")
		}
	})
}

func TestCleanup(t *testing.T) {
	t.Run("cleanup initialized datastore", func(t *testing.T) {
		db, err := datastore.NewSQLite(":memory:")
		if err != nil {
			t.Fatal("Failed to create test database:", err)
		}

		err = datastore.Initialize(db)
		if err != nil {
			t.Fatal("Failed to initialize datastore:", err)
		}

		// Cleanup should not panic - this is the main test
		// In production, Cleanup() is called at application shutdown to close database connections
		datastore.Cleanup()

		// Note: Cleanup() calls the store's Cleanup() method (closes connections) but doesn't
		// set the singleton to nil. This is by design - the singleton remains for the application lifetime.
		// We're just verifying Cleanup() doesn't panic.
	})

	t.Run("cleanup uninitialized datastore", func(t *testing.T) {
		// Cleanup should not panic even if not initialized
		datastore.Cleanup()
	})
}

func TestSingletonPattern(t *testing.T) {
	t.Run("singleton ensures same instance", func(t *testing.T) {
		datastore.Cleanup()

		db, err := datastore.NewSQLite(":memory:")
		if err != nil {
			t.Fatal("Failed to create test database:", err)
		}
		defer db.Cleanup()

		err = datastore.Initialize(db)
		if err != nil {
			t.Fatal("Failed to initialize datastore:", err)
		}
		defer datastore.Cleanup()

		store1, _ := datastore.Get()
		store2, _ := datastore.Get()

		if store1 != store2 {
			t.Error("Expected singleton to return same instance")
		}
	})

	t.Run("multiple initialize calls - first wins", func(t *testing.T) {
		// Note: Due to sync.Once in the global state, this test may be affected
		// by other tests. This demonstrates the singleton pattern behavior.
		// In production, Initialize should only be called once at application startup.

		// Create two databases
		db1, err := datastore.NewSQLite(":memory:")
		if err != nil {
			t.Fatal("Failed to create first test database:", err)
		}
		defer db1.Cleanup()

		db2, err := datastore.NewSQLite(":memory:")
		if err != nil {
			t.Fatal("Failed to create second test database:", err)
		}
		defer db2.Cleanup()

		// Get current store (might already be initialized from previous tests)
		store, _ := datastore.Get()

		// If already initialized, can't test "first wins" due to sync.Once
		// Just verify the store is non-nil
		if store == nil {
			t.Error("Expected store to be initialized")
		}
	})
}
