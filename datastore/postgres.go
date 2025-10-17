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
	sqlDB *sql.DB
	db    *bun.DB
}

// NewPostgres creates a new PostgreSQL datastore
func NewPostgres(dsn string) (*PostgreSQL, error) {
	connector := pgdriver.NewConnector(pgdriver.WithDSN(dsn))
	sqlDB := sql.OpenDB(connector)

	db := bun.NewDB(sqlDB, pgdialect.New())

	return &PostgreSQL{
		sqlDB: sqlDB,
		db:    db,
	}, nil
}

func (p *PostgreSQL) GetDB() *bun.DB {
	return p.db
}

func (p *PostgreSQL) GetTimeout() time.Duration {
	return 30 * time.Second
}

func (p *PostgreSQL) Cleanup() {
	if p.db != nil {
		p.db.Close()
	}
	if p.sqlDB != nil {
		p.sqlDB.Close()
	}
}
