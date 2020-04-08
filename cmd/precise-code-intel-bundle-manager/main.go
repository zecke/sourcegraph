package main

import (
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/inconshreveable/log15"
	"github.com/sourcegraph/sourcegraph/cmd/precise-code-intel-bundle-manager/server"
	"github.com/sourcegraph/sourcegraph/internal/debugserver"
	"github.com/sourcegraph/sourcegraph/internal/env"
	"github.com/sourcegraph/sourcegraph/internal/sqliteutil"
	"github.com/sourcegraph/sourcegraph/internal/tracer"
)

func main() {
	env.Lock()
	env.HandleHelpFlag()
	tracer.Init()

	sqliteutil.MustRegisterSqlite3WithPcre()

	var (
		bundleDir                = mustGet(rawBundleDir, "BUNDLE_DIR")
		databaseCacheSize        = mustParseInt(rawDatabaseCacheSize, "CONNECTION_CACHE_CAPACITY")
		documentDataCacheSize    = mustParseInt(rawDocumentDataCacheSize, "DOCUMENT_CACHE_CAPACITY")
		resultChunkDataCacheSize = mustParseInt(rawResultChunkDataCacheSize, "RESULT_CHUNK_CACHE_CAPACITY")
		desiredPercentFree       = mustParsePercent(rawDesiredPercentFree, "DESIRED_PERCENT_FREE")
		janitorInterval          = mustParseInterval(rawJanitorInterval, "JANITOR_INTERVAL")
		maxUnconvertedUploadAge  = mustParseInterval(rawMaxUnconvertedUploadAge, "MAX_UNCONVERTED_UPLOAD_AGE")
	)

	for _, dir := range []string{"", "uploads", "dbs"} {
		path := filepath.Join(bundleDir, dir)
		if err := os.MkdirAll(path, os.ModePerm); err != nil {
			log.Fatalf("failed to create directory %s: %s", path, err)
		}
	}

	host := ""
	if env.InsecureDev {
		host = "127.0.0.1"
	}

	server, err := server.New(server.ServerOpts{
		Host:                     host,
		Port:                     3187,
		BundleDir:                bundleDir,
		DatabaseCacheSize:        databaseCacheSize,
		DocumentDataCacheSize:    documentDataCacheSize,
		ResultChunkDataCacheSize: resultChunkDataCacheSize,
		DesiredPercentFree:       desiredPercentFree,
		MaxUnconvertedUploadAge:  maxUnconvertedUploadAge,
	})
	if err != nil {
		log.Fatal(err)
	}

	go server.Start()
	go debugserver.Start()

	go func() {
		for {
			if err := server.Janitor(); err != nil {
				log15.Error("Failed to run cleanup process", "error", err)
			}

			time.Sleep(janitorInterval)
		}
	}()

	waitForSignal()
}

func waitForSignal() {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGHUP)

	for i := 0; i < 2; i++ {
		<-signals
	}

	os.Exit(0)
}
