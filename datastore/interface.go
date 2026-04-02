// Package datastore provides database abstraction and generic CRUD operations
// for different database backends including PostgreSQL and SQLite.
package datastore

import (
	"time"

	"github.com/uptrace/bun"
)

// Store defines the contract for any data store implementation
type Store interface {
	GetDB() *bun.DB
	GetTimeout() time.Duration
	IlikeOp() string
	Cleanup()
}
