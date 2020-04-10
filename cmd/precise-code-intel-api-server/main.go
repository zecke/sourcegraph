package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/sourcegraph/sourcegraph/cmd/precise-code-intel-api-server/server"
	"github.com/sourcegraph/sourcegraph/cmd/precise-code-intel-api-server/server/db"
	"github.com/sourcegraph/sourcegraph/internal/conf"
	"github.com/sourcegraph/sourcegraph/internal/debugserver"
	"github.com/sourcegraph/sourcegraph/internal/env"
	"github.com/sourcegraph/sourcegraph/internal/tracer"
)

func main() {
	env.Lock()
	env.HandleHelpFlag()
	tracer.Init()

	var (
		janitorInterval  = mustParseInterval(rawJanitorInterval, "JANITOR_INTERVAL")
		bundelManagerURL = mustGet(rawBundleManagerURL, "BUNDLE_MANAGER_URL")
	)

	db := mustInitializeDatabase()

	host := ""
	if env.InsecureDev {
		host = "127.0.0.1"
	}

	serverInst := server.New(server.ServerOpts{
		Host:             host,
		Port:             3186,
		BundleManagerURL: bundelManagerURL,
		DB:               db,
	})

	janitorInst := server.NewJanitor(server.JanitorOpts{
		DB:              db,
		JanitorInterval: janitorInterval,
	})

	go serverInst.Start()
	go janitorInst.Start()
	go debugserver.Start()
	waitForSignal()
}

func mustInitializeDatabase() *db.DB {
	postgresDSN := conf.Get().ServiceConnections.PostgresDSN
	conf.Watch(func() {
		if newDSN := conf.Get().ServiceConnections.PostgresDSN; postgresDSN != newDSN {
			log.Fatalf("Detected repository DSN change, restarting to take effect: %s", newDSN)
		}
	})

	db, err := db.New(postgresDSN)
	if err != nil {
		log.Fatalf("failed to initialize db store: %s", err)
	}

	return db
}

func waitForSignal() {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGHUP)

	for i := 0; i < 2; i++ {
		<-signals
	}

	os.Exit(0)
}
