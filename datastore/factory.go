package datastore

import (
	"fmt"
	"sync"
)

var (
	// singleton instance for datastore
	singleton Store
	// once ensures Initialize is only called once
	once sync.Once
	// initErr stores any initialization error
	initErr error
)

// Initialize sets the global datastore singleton
// This must be called during application startup before any handlers are used
// It is safe to call from multiple goroutines, but only the first call will take effect
func Initialize(store Store) error {
	once.Do(func() {
		if store == nil {
			initErr = fmt.Errorf("store cannot be nil")
			return
		}
		singleton = store
	})
	return initErr
}

// Get returns the singleton datastore instance
// Returns an error if Initialize() hasn't been called
func Get() (Store, error) {
	if singleton == nil {
		return nil, fmt.Errorf("datastore not initialized - call datastore.Initialize() first")
	}
	return singleton, nil
}

// Cleanup cleans up the datastore singleton
// Call this when shutting down the application
func Cleanup() {
	if singleton != nil {
		singleton.Cleanup()
	}
}
