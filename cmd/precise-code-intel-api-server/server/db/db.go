package db

import (
	"database/sql"

	"github.com/sourcegraph/sourcegraph/internal/db/dbutil"
)

type DB struct {
	db *sql.DB
}

func New(postgresDSN string) (*DB, error) {
	db, err := dbutil.NewDB(postgresDSN, "precise-code-intel-api-server")
	if err != nil {
		return nil, err
	}

	return &DB{db: db}, nil
}
