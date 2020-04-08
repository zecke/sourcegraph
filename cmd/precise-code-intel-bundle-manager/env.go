package main

import (
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/sourcegraph/sourcegraph/internal/env"
)

var (
	envPrefix                   = "PRECISE_CODE_INTEL"
	rawBundleDir                = envGet("BUNDLE_DIR", "/lsif-storage", "Root dir containing uploads and converted bundles.")
	rawDatabaseCacheSize        = envGet("CONNECTION_CACHE_CAPACITY", "100", "Number of SQLite connections that can be opened at once.")
	rawDocumentDataCacheSize    = envGet("DOCUMENT_CACHE_CAPACITY", "100", "Maximum number of decoded documents that can be held in memory at once.")
	rawResultChunkDataCacheSize = envGet("RESULT_CHUNK_CACHE_CAPACITY", "100", "Maximum number of decoded result chunks that can be held in memory at once.")
	rawDesiredPercentFree       = envGet("DESIRED_PERCENT_FREE", "10", "Target percentage of free space on disk.")
	rawJanitorInterval          = envGet("JANITOR_INTERVAL", "1m", "Interval between cleanup runs.")
	rawMaxUnconvertedUploadAge  = envGet("MAX_UNCONVERTED_UPLOAD_AGE", "1d", "The maximum time an unconverted upload can sit on disk.")
)

func envGet(name, defaultValue, description string) string {
	return env.Get(fmt.Sprintf("%s_%s", envPrefix, name), defaultValue, description)
}

func mustGet(rawValue, name string) string {
	if rawValue == "" {
		log.Fatalf("invalid value %q for %s_%s: no value supplied", rawValue, envPrefix, name)
	}

	return rawValue
}

func mustParseInt(rawValue, name string) int {
	i, err := strconv.ParseInt(rawValue, 10, 64)
	if err != nil {
		log.Fatalf("invalid int %q for %s_%s: %s", rawValue, envPrefix, name, err)
	}

	return int(i)
}

func mustParsePercent(rawValue, name string) int {
	p := mustParseInt(rawValue, name)
	if p < 0 || p > 100 {
		log.Fatalf("invalid percent %q for %s_%s: must be 0 <= p <= 100", rawValue, envPrefix, name)
	}

	return p
}

func mustParseInterval(rawValue, name string) time.Duration {
	d, err := time.ParseDuration(rawValue)
	if err != nil {
		log.Fatalf("invalid duration %q for %s_%s: %s", rawValue, envPrefix, name, err)
	}

	return d
}
