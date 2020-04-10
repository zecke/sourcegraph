package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/sourcegraph/sourcegraph/cmd/precise-code-intel-api-server/server/db"
)

func (s *Server) findClosestDatabase(repositoryID int, commit, file string) ([]db.Dump, error) {
	candidates, err := s.db.FindClosestDumps(repositoryID, commit, file)
	if err != nil {
		return nil, err
	}

	var dumps []db.Dump
	for _, dump := range candidates {
		db := Database{
			bundleManagerURL: s.bundleManagerURL,
			dumpID:           dump.ID,
		}

		// TODO - do these requests in parallel
		exists, err := db.Exists(pathToRoot(dump.Root, file))
		if err != nil {
			return nil, err
		}

		if exists {
			dumps = append(dumps, dump)
		}
	}

	return dumps, nil
}

func (s *Server) lookupMoniker(dumpID int, path string, moniker MonikerData, model string, skip, take *int) ([]Location, int, error) {
	if moniker.PackageInformationID == "" {
		return nil, 0, nil
	}

	db := Database{
		dumpID:           dumpID,
		bundleManagerURL: s.bundleManagerURL,
	}
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

	db2 := Database{
		dumpID:           dump.ID,
		bundleManagerURL: s.bundleManagerURL,
	}
	locations, count, err := db2.MonikerResults(model, moniker.Scheme, moniker.Identifier, skip, take)
	if err != nil {
		return nil, 0, err
	}

	var resolved []Location
	for _, l := range locations {
		resolved = append(resolved, Location{
			DumpID: l.DumpID,
			Path:   dump.Root + l.Path,
			Range:  l.Range,
		})
	}

	return resolved, count, err
}

func (s *Server) getRefs(repositoryID int, commit string, limit int, cursor Cursor) (PaginatedLocations, error) {
	return s.handlePagination(repositoryID, commit, 20, limit, cursor)
}

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
	dump, db, exists, err := s.getDumpAndDatabase(uploadID)
	if err != nil {
		return Cursor{}, err
	}
	if !exists {
		return Cursor{}, ErrMissingDump
	}

	pathInDb := pathToDb(dump.Root, file)

	rangeMonikers, err := db.MonikersByPosition(pathInDb, line, character)
	if err != nil {
		return Cursor{}, err
	}

	var flattened []MonikerData
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

func (s *Server) getDefs(file string, line, character, uploadID int) ([]LocationThingers, error) {
	dump, db, exists, err := s.getDumpAndDatabase(uploadID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrMissingDump
	}

	return s.getDefsRaw(dump, db, pathToDb(dump.Root, file), line, character)
}

func (s *Server) getDefsRaw(dump db.Dump, db Database, pathInDb string, line, character int) ([]LocationThingers, error) {
	locations, err := db.Definitions(pathInDb, line, character)
	if err != nil {
		return nil, err
	}

	if len(locations) > 0 {
		return s.resolveLocations(dump.Root, locations), nil
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

				x := s.resolveLocations("", results)
				if len(x) > 0 {
					return x, nil
				}
			} else {
				// This symbol was not imported from another database. We search the definitions
				// table of our own database in case there was a definition that wasn't properly
				// attached to a result set but did have the correct monikers attached.

				results, _, err := db.MonikerResults("definition", moniker.Scheme, moniker.Identifier, nil, nil)
				if err != nil {
					return nil, err
				}

				x := s.resolveLocations(dump.Root, results)
				if len(x) > 0 {
					return x, nil
				}
			}
		}
	}

	return nil, nil
}

func (s *Server) getHover(file string, line, character, uploadID int) (string, Range, bool, error) {
	dump, db, exists, err := s.getDumpAndDatabase(uploadID)
	if err != nil {
		return "", Range{}, false, err
	}
	if !exists {
		return "", Range{}, false, ErrMissingDump
	}

	pathx := pathToDb(dump.Root, file)
	text, rn, exists, err := db.Hover(pathx, line, character)
	if err != nil {
		return "", Range{}, false, err
	}
	if exists {
		return text, rn, true, nil
	}

	defs, err := s.getDefsRaw(dump, db, pathx, line, character)
	if err != nil {
		return "", Range{}, false, err
	}
	resolved, err := s.resolveLocations2(defs)
	if err != nil {
		return "", Range{}, false, err
	}

	if len(resolved) == 0 {
		return "", Range{}, false, nil
	}

	db2 := Database{
		dumpID:           resolved[0].Dump.ID,
		bundleManagerURL: s.bundleManagerURL,
	}

	p := pathToDb(resolved[0].Dump.Root, resolved[0].Path)
	return db2.Hover(p, resolved[0].Range.Start.Line, resolved[0].Range.Start.Character)
}

func (s *Server) lookupPackageInformation(dumpID int, path string, moniker MonikerData) (PackageInformationData, bool, error) {
	if moniker.PackageInformationID == "" {
		return PackageInformationData{}, false, nil
	}

	db := Database{
		dumpID:           dumpID,
		bundleManagerURL: s.bundleManagerURL,
	}
	pi, err := db.PackageInformation(path, moniker.PackageInformationID)
	if err != nil {
		return PackageInformationData{}, false, err
	}

	return pi, true, nil
}

func (s *Server) getDumpAndDatabase(uploadID int) (db.Dump, Database, bool, error) {
	dump, exists, err := s.db.GetDumpByID(uploadID)
	if err != nil {
		return db.Dump{}, Database{}, false, err
	}
	if !exists {
		return db.Dump{}, Database{}, false, nil
	}

	db := Database{
		bundleManagerURL: s.bundleManagerURL,
		dumpID:           dump.ID,
	}

	return dump, db, true, nil
}

// TODO - lol

func pathToDb(root, path string) string {
	if strings.HasPrefix(path, root) {
		return strings.TrimPrefix(path, root)
	}
	return path
}

func pathToRoot(root, file string) string {
	if strings.HasPrefix(file, root) {
		return strings.TrimPrefix(file, root)
	}
	return file
}
