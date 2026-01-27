package datastore

import (
	"database/sql"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

// PostgreSQL implements Store for PostgreSQL databases
type PostgreSQL struct {
	sqlDB          *sql.DB
	db             *bun.DB
	ownsConnection bool
}

// NewPostgres creates a new PostgreSQL datastore.
// The datastore owns the connection and Cleanup() will close it.
func NewPostgres(dsn string) (*PostgreSQL, error) {
	connector := pgdriver.NewConnector(pgdriver.WithDSN(dsn))
	sqlDB := sql.OpenDB(connector)

	db := bun.NewDB(sqlDB, pgdialect.New())

	return &PostgreSQL{
		sqlDB:          sqlDB,
		db:             db,
		ownsConnection: true,
	}, nil
}

// NewPostgresWithDB creates a PostgreSQL datastore from an existing *sql.DB.
// Use this when you need to manage the database connection externally,
// such as with Vault rotating credentials or custom connection pooling.
//
// IMPORTANT: The caller retains ownership of the *sql.DB connection.
// Cleanup() will close the bun.DB wrapper but will NOT close the underlying
// *sql.DB - you must close it yourself when done.
func NewPostgresWithDB(sqlDB *sql.DB) *PostgreSQL {
	return &PostgreSQL{
		sqlDB:          sqlDB,
		db:             bun.NewDB(sqlDB, pgdialect.New()),
		ownsConnection: false,
	}
}

func (p *PostgreSQL) GetDB() *bun.DB {
	return p.db
}

func (p *PostgreSQL) GetTimeout() time.Duration {
	return 30 * time.Second
}

func (p *PostgreSQL) Cleanup() {
	if p.ownsConnection {
		if p.db != nil {
			_ = p.db.Close()
		}
		if p.sqlDB != nil {
			_ = p.sqlDB.Close()
		}
	}
}
