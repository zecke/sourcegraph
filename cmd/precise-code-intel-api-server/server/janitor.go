package server

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/inconshreveable/log15"
)

const StalledUploadMaxAge = time.Second * 5

type Janitor struct {
	db              *sql.DB
	janitorInterval time.Duration
}

type JanitorOpts struct {
	DB              *sql.DB
	JanitorInterval time.Duration
}

func NewJanitor(opts JanitorOpts) *Janitor {
	return &Janitor{
		db:              opts.DB,
		janitorInterval: opts.JanitorInterval,
	}
}

func (j *Janitor) Start() {
	for {
		if err := j.step(); err != nil {
			log15.Error("Failed to run janitor process", "error", err)
		}

		time.Sleep(j.janitorInterval)
	}
}

func (j *Janitor) step() error {
	query := `
		UPDATE lsif_uploads u SET state = 'queued', started_at = null WHERE id = ANY(
			SELECT id FROM lsif_uploads
			WHERE state = 'processing' AND started_at < now() - ($1 * interval '1 second')
			FOR UPDATE SKIP LOCKED
		)
		RETURNING u.id
	`

	rows, err := j.db.QueryContext(context.Background(), query, StalledUploadMaxAge/time.Second)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return err
		}

		// TODO - log instead
		fmt.Printf("> RESET %#v\n", id)
	}

	return nil
}
