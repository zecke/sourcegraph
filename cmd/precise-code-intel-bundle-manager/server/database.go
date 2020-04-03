package server

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

type Database struct {
	db *sqlx.DB
}

// TODO - make errorable
func NewDatabase(filename string) *Database {
	// TODO - handle registration
	db, err := sqlx.Open("sqlite3_with_pcre", filename)
	if err != nil {
		// TODO - handle error
		panic(err.Error())
	}

	return &Database{db: db}
}

// TODO - make a pool so this is actually called
func (db *Database) Close() error {
	return db.Close()
}

func (db *Database) Exists(path string) (interface{}, error) {
	var data string
	err := db.db.Select(&data, "SELECT data FROM documents WHERE path = :path", path)
	if err != nil {
		return nil, err
	}

	// TODO
	return nil, fmt.Errorf("unimplemented")
}

func (db *Database) Definitions() (interface{}, error) {
	// TODO
	return nil, fmt.Errorf("unimplemented")
}

func (db *Database) References() (interface{}, error) {
	// TODO
	return nil, fmt.Errorf("unimplemented")
}

func (db *Database) MonikerByPosition() (interface{}, error) {
	// TODO
	return nil, fmt.Errorf("unimplemented")
}

func (db *Database) MonikerResults() (interface{}, error) {
	// TODO
	return nil, fmt.Errorf("unimplemented")
}

func (db *Database) PackageInformation() (interface{}, error) {
	// TODO
	return nil, fmt.Errorf("unimplemented")
}
