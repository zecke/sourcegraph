package server

import (
	"strings"
	"testing"
	"time"
)

// CloseLoopTestIterations is the maximum number of iterations to spin while
// waiting for a database hand to close after it has been evicted from the
// cache.
const CloseLoopTestIterations = 100

func TestConnectionCacheEvictionWhileHeld(t *testing.T) {
	cache, err := NewDatabaseCache(2)
	if err != nil {
		t.Fatalf("unexpected error creating database cache: %s", err)
	}

	// database handle that outlives its time in the cache
	var dbRef *Database

	// cache: foo
	if err := cache.WithDatabase("foo", openTestDatabase, func(db1 *Database) error {
		dbRef = db1

		// cache: bar,foo
		if err := cache.WithDatabase("bar", openTestDatabase, noopHandler); err != nil {
			return err
		}

		// cache: baz, bar
		// note: foo was evicted but should not be closed
		if err := cache.WithDatabase("baz", openTestDatabase, noopHandler); err != nil {
			return err
		}

		// cache: foo, bar
		// note: this version of foo should be a fresh connection
		return cache.WithDatabase("foo", openTestDatabase, func(db2 *Database) error {
			if db1 == db2 {
				t.Fatalf("unexpected cached database")
			}

			assertLSIFVersion(t, db1)
			assertLSIFVersion(t, db2)
			return nil
		})
	}); err != nil {
		t.Fatalf("unexpected error during test: %s", err)
	}

	// evicted database is closed after use
	assertClosed(t, dbRef)
}

func noopHandler(_ *Database) error {
	return nil
}

func getLSIFVersion(db *Database) (string, error) {
	var version string
	err := db.db.Get(&version, "SELECT lsifVersion FROM meta LIMIT 1")
	return version, err
}

func assertLSIFVersion(t *testing.T, db *Database) {
	if version, err := getLSIFVersion(db); err != nil {
		t.Fatalf("unexpected error querying db: %s", err)
	} else if version != "0.4.3" {
		t.Errorf("unexpected lsifVersion: want=%s have=%s", "0.4.3", version)
	}
}

func assertClosed(t *testing.T, db *Database) {
	for i := 0; i < 200; i++ {
		if _, err := getLSIFVersion(db); err != nil {
			break
		}

		time.Sleep(time.Millisecond)
	}

	if _, err := getLSIFVersion(db); err == nil {
		t.Fatalf("unexpected nil error")
	} else if !strings.Contains(err.Error(), "database is closed") {
		t.Fatalf("unexpected error: want=%s have=%s", "database is closed", err)
	}
}
