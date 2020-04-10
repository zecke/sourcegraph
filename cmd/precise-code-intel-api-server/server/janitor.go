package server

import (
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
	ids, err := j.cleanOld()
	if err != nil {
		return err
	}

	for _, id := range ids {
		// TODO - log instead
		fmt.Printf("> RESET %#v\n", id)
	}

	return nil
}
