package server

import (
	"fmt"
	"time"

	"github.com/inconshreveable/log15"
	"github.com/sourcegraph/sourcegraph/cmd/precise-code-intel-api-server/server/db"
)

type Janitor struct {
	db              *db.DB
	janitorInterval time.Duration
}

type JanitorOpts struct {
	DB              *db.DB
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
	ids, err := j.db.ResetStalled()
	if err != nil {
		return err
	}

	for _, id := range ids {
		// TODO - log instead
		fmt.Printf("> RESET %#v\n", id)
	}

	return nil
}
