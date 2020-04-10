package server

import "github.com/sourcegraph/sourcegraph/cmd/precise-code-intel-api-server/server/db"

func applyBloomFilter(refs []db.Reference, identifier string, limit int) ([]db.Reference, int) {
	return refs, len(refs) // TODO - implement
}
