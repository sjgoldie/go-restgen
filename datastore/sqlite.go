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
	sqlDB *sql.DB
	db    *bun.DB
}

// NewSQLite creates a new SQLite datastore
// Use ":memory:" for in-memory database, or provide a file path
func NewSQLite(dsn string) (*SQLite, error) {
	sqlDB, err := sql.Open(sqliteshim.ShimName, dsn)
	if err != nil {
		return nil, err
	}

	db := bun.NewDB(sqlDB, sqlitedialect.New())

	return &SQLite{
		sqlDB: sqlDB,
		db:    db,
	}, nil
}

func (s *SQLite) GetDB() *bun.DB {
	return s.db
}

func (s *SQLite) GetTimeout() time.Duration {
	return 5 * time.Second
}

func (s *SQLite) Cleanup() {
	if s.db != nil {
		s.db.Close()
	}
	if s.sqlDB != nil {
		s.sqlDB.Close()
	}
}
