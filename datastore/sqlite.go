package datastore

import (
	"database/sql"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
)

// SQLite implements Store for SQLite databases
type SQLite struct {
	sqlDB          *sql.DB
	db             *bun.DB
	ownsConnection bool
}

// NewSQLite creates a new SQLite datastore.
// Use ":memory:" for in-memory database, or provide a file path.
// The datastore owns the connection and Cleanup() will close it.
func NewSQLite(dsn string) (*SQLite, error) {
	sqlDB, err := sql.Open(sqliteshim.ShimName, dsn)
	if err != nil {
		return nil, err
	}

	db := bun.NewDB(sqlDB, sqlitedialect.New())

	return &SQLite{
		sqlDB:          sqlDB,
		db:             db,
		ownsConnection: true,
	}, nil
}

// NewSQLiteWithDB creates a SQLite datastore from an existing *sql.DB.
// Use this when you need to manage the database connection externally,
// such as with Vault rotating credentials or custom connection pooling.
//
// IMPORTANT: The caller retains ownership of the *sql.DB connection.
// Cleanup() is a no-op for a datastore created this way; it closes neither
// the bun.DB wrapper nor the underlying *sql.DB. Close the *sql.DB yourself
// when done.
func NewSQLiteWithDB(sqlDB *sql.DB) *SQLite {
	return &SQLite{
		sqlDB:          sqlDB,
		db:             bun.NewDB(sqlDB, sqlitedialect.New()),
		ownsConnection: false,
	}
}

func (s *SQLite) GetDB() *bun.DB {
	return s.db
}

func (s *SQLite) GetTimeout() time.Duration {
	return 5 * time.Second
}

func (s *SQLite) IlikeOp() string {
	return "LIKE"
}

func (s *SQLite) Cleanup() {
	if s.ownsConnection {
		if s.db != nil {
			_ = s.db.Close()
		}
		if s.sqlDB != nil {
			_ = s.sqlDB.Close()
		}
	}
}
