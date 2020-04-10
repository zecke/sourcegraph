package server

import (
	"github.com/sourcegraph/sourcegraph/cmd/precise-code-intel-api-server/server/bundles"
	"github.com/sourcegraph/sourcegraph/cmd/precise-code-intel-api-server/server/db"
)

func (s *Server) getDefs(file string, line, character, uploadID int) ([]ResolvedLocation, error) {
	dump, bundleClient, exists, err := s.getDumpAndBundleClient(uploadID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrMissingDump
	}

	return s.getDefsRaw(dump, bundleClient, pathRelativeToRoot(dump.Root, file), line, character)
}

func (s *Server) getDefsRaw(dump db.Dump, db *bundles.BundleClient, pathInDb string, line, character int) ([]ResolvedLocation, error) {
	locations, err := db.Definitions(pathInDb, line, character)
	if err != nil {
		return nil, err
	}

	if len(locations) > 0 {
		return resolveLocations(dump, locations), nil
	}

	rangeMonikers, err := db.MonikersByPosition(pathInDb, line, character)
	if err != nil {
		return nil, err
	}

	for _, monikers := range rangeMonikers {
		for _, moniker := range monikers {
			if moniker.Kind == "import" {
				results, _, err := s.lookupMoniker(dump.ID, pathInDb, moniker, "definition", nil, nil)
				if err != nil {
					return nil, err
				}

				if len(results) > 0 {
					// TODO - unify these
					return s.resolveLocations(results)
				}
			} else {
				// This symbol was not imported from another database. We search the definitions
				// table of our own database in case there was a definition that wasn't properly
				// attached to a result set but did have the correct monikers attached.

				results, _, err := db.MonikerResults("definition", moniker.Scheme, moniker.Identifier, nil, nil)
				if err != nil {
					return nil, err
				}

				if len(results) > 0 {
					return resolveLocations(dump, results), nil
				}
			}
		}
	}

	return nil, nil
}
