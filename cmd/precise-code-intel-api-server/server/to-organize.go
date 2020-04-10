package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/sourcegraph/sourcegraph/cmd/precise-code-intel-api-server/server/bundles"
	"github.com/sourcegraph/sourcegraph/cmd/precise-code-intel-api-server/server/db"
)

func (s *Server) findClosestDatabase(repositoryID int, commit, file string) ([]db.Dump, error) {
	candidates, err := s.db.FindClosestDumps(repositoryID, commit, file)
	if err != nil {
		return nil, err
	}

	var dumps []db.Dump
	for _, dump := range candidates {
		// TODO - do these requests in parallel
		exists, err := s.bundleManagerClient.BundleClient(dump.ID).Exists(pathRelativeToRoot(dump.Root, file))
		if err != nil {
			return nil, err
		}

		if exists {
			dumps = append(dumps, dump)
		}
	}

	return dumps, nil
}

func (s *Server) getDumpAndBundleClient(uploadID int) (db.Dump, *bundles.BundleClient, bool, error) {
	dump, exists, err := s.db.GetDumpByID(uploadID)
	if err != nil {
		return db.Dump{}, nil, false, err
	}
	if !exists {
		return db.Dump{}, nil, false, nil
	}

	return dump, s.bundleManagerClient.BundleClient(dump.ID), true, nil
}

func pathRelativeToRoot(root, path string) string {
	if strings.HasPrefix(path, root) {
		return strings.TrimPrefix(path, root)
	}
	return path
}

///
///

var ErrMissingDump = fmt.Errorf("no dump")

func (s *Server) decodeCursor(r *http.Request) (Cursor, error) {
	q := r.URL.Query()
	repositoryID, _ := strconv.Atoi(q.Get("repositoryId"))
	commit := q.Get("commit")
	file := q.Get("path")
	line, _ := strconv.Atoi(q.Get("line"))
	character, _ := strconv.Atoi(q.Get("character"))
	uploadID, _ := strconv.Atoi(q.Get("uploadId"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	encoded := q.Get("cursor")

	if encoded != "" {
		cursor, err := decodeCursor(encoded)
		if err != nil {
			return Cursor{}, err
		}

		return cursor, nil
	}

	return s.makeCursor(repositoryID, commit, file, line, character, uploadID, limit)
}

func (s *Server) makeCursor(repositoryID int, commit, file string, line, character, uploadID int, limit int) (Cursor, error) {
	dump, bundleClient, exists, err := s.getDumpAndBundleClient(uploadID)
	if err != nil {
		return Cursor{}, err
	}
	if !exists {
		return Cursor{}, ErrMissingDump
	}

	pathInDb := pathRelativeToRoot(dump.Root, file)

	rangeMonikers, err := bundleClient.MonikersByPosition(pathInDb, line, character)
	if err != nil {
		return Cursor{}, err
	}

	var flattened []bundles.MonikerData
	for _, monikers := range rangeMonikers {
		flattened = append(flattened, monikers...)
	}

	newCursor := Cursor{
		Phase:       "same-dump",
		DumpID:      dump.ID,
		Path:        pathInDb,
		Line:        line,
		Character:   character,
		Monikers:    flattened,
		SkipResults: 0,
	}

	return newCursor, nil
}

func (s *Server) lookupMoniker(dumpID int, path string, moniker bundles.MonikerData, model string, skip, take *int) ([]bundles.Location, int, error) {
	if moniker.PackageInformationID == "" {
		return nil, 0, nil
	}

	db := s.bundleManagerClient.BundleClient(dumpID)
	pid, err := db.PackageInformation(path, moniker.PackageInformationID)
	if err != nil {
		return nil, 0, err
	}

	dump, exists, err := s.db.GetPackage(moniker.Scheme, pid.Name, pid.Version)
	if err != nil {
		return nil, 0, err
	}
	if !exists {
		return nil, 0, nil
	}

	locations, count, err := s.bundleManagerClient.BundleClient(dump.ID).MonikerResults(model, moniker.Scheme, moniker.Identifier, skip, take)
	if err != nil {
		return nil, 0, err
	}

	// TODO - DRY
	var resolved []bundles.Location
	for _, l := range locations {
		resolved = append(resolved, bundles.Location{
			DumpID: l.DumpID,
			Path:   dump.Root + l.Path,
			Range:  l.Range,
		})
	}

	return resolved, count, err
}

func (s *Server) lookupPackageInformation(dumpID int, path string, moniker bundles.MonikerData) (bundles.PackageInformationData, bool, error) {
	if moniker.PackageInformationID == "" {
		return bundles.PackageInformationData{}, false, nil
	}

	db := s.bundleManagerClient.BundleClient(dumpID)
	pi, err := db.PackageInformation(path, moniker.PackageInformationID)
	if err != nil {
		return bundles.PackageInformationData{}, false, err
	}

	return pi, true, nil
}
