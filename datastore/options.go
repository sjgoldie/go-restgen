package datastore

import "time"

// Default per-query timeouts applied when WithTimeout is not given.
const (
	DefaultPostgresTimeout = 30 * time.Second
	DefaultSQLiteTimeout   = 5 * time.Second
)

// Option configures a Store constructor.
type Option func(*storeConfig)

type storeConfig struct {
	timeout time.Duration
}

// WithTimeout sets the per-query timeout the datastore applies to every
// operation on the store. Values of zero or less are ignored and the store's
// default applies: DefaultPostgresTimeout for PostgreSQL, DefaultSQLiteTimeout
// for SQLite.
func WithTimeout(d time.Duration) Option {
	return func(c *storeConfig) {
		if d > 0 {
			c.timeout = d
		}
	}
}

func applyOptions(defaultTimeout time.Duration, opts []Option) storeConfig {
	cfg := storeConfig{timeout: defaultTimeout}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
