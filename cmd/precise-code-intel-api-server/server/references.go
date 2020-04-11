package server

import (
	"fmt"

	"github.com/sourcegraph/sourcegraph/cmd/precise-code-intel-api-server/server/db"
)

// type PaginatedLocations struct {
// 	locations []ResolvedLocation
// 	newCursor *Cursor // TODO - no pointers
// }

func (s *Server) getRefs(repositoryID int, commit string, limit int, cursor Cursor) ([]ResolvedLocation, Cursor, bool, error) {
	rpr := &ReferencePageResolver{
		s:            s,
		repositoryID: repositoryID,
		commit:       commit,
		limit:        limit,
	}

	return rpr.resolvePage(cursor)
}

//
//
//
//

type ReferencePageResolver struct {
	s               *Server // TODO - do we need this?
	repositoryID    int
	commit          string
	remoteDumpLimit int
	limit           int
}

func (s *ReferencePageResolver) resolvePage(cursor Cursor) ([]ResolvedLocation, Cursor, bool, error) {
	locations, newCursor, hasNewCursor, err := s.dispatchCursorHandler(cursor)
	if err != nil {
		return nil, Cursor{}, false, nil
	}

	if hasNewCursor && s.limit >= 0 {
		var newLocations []ResolvedLocation
		newLocations, newCursor, hasNewCursor, err = s.dispatchCursorHandler(newCursor)
		if err != nil {
			return nil, Cursor{}, false, err
		}

		locations = append(locations, newLocations...)
	}

	return locations, newCursor, hasNewCursor, nil
}

func (s *ReferencePageResolver) dispatchCursorHandler(cursor Cursor) ([]ResolvedLocation, Cursor, bool, error) {
	fns := map[string]func(Cursor) ([]ResolvedLocation, Cursor, bool, error){
		"same-dump":           s.handleSameDumpCursor,
		"definition-monikers": s.handleDefinitionMonikersCursor,
		"same-repo":           s.handleSameRepoCursor,
		"remote-repo":         s.handleRemoteRepoCursor,
	}

	fn, ok := fns[cursor.Phase]
	if !ok {
		return nil, Cursor{}, false, fmt.Errorf("malformed cursor")
	}

	return fn(cursor)
}

func (s *ReferencePageResolver) handleSameDumpCursor(cursor Cursor) ([]ResolvedLocation, Cursor, bool, error) {
	locations, newCursor, hasNewCursor, err := s.s.performSameDumpReferences(s.limit, cursor)
	if err != nil {
		return nil, Cursor{}, false, err
	}
	if hasNewCursor {
		return locations, newCursor, true, nil
	}

	newCursor = Cursor{
		DumpID:      cursor.DumpID,
		Phase:       "definition-monikers",
		Path:        cursor.Path,
		Line:        cursor.Line,
		Character:   cursor.Character,
		Monikers:    cursor.Monikers,
		SkipResults: 0,
	}
	return locations, newCursor, true, nil
}

func (s *ReferencePageResolver) handleDefinitionMonikersCursor(cursor Cursor) ([]ResolvedLocation, Cursor, bool, error) {
	locations, newCursor, hasNewCursor, err := s.s.performDefinitionMonikersReference(s.limit, cursor)
	if err != nil {
		return nil, Cursor{}, false, err
	}
	if hasNewCursor {
		return locations, newCursor, true, nil
	}

	for _, moniker := range cursor.Monikers {
		packageInformation, exists, err := s.s.lookupPackageInformation(cursor.DumpID, cursor.Path, moniker)
		if err != nil {
			return nil, Cursor{}, false, err
		}
		if exists {
			newCursor = Cursor{
				DumpID:                 cursor.DumpID,
				Phase:                  "same-repo",
				Scheme:                 moniker.Scheme,
				Identifier:             moniker.Identifier,
				Name:                   packageInformation.Name,
				Version:                packageInformation.Version,
				DumpIDs:                nil,
				TotalDumpsWhenBatching: 0,
				SkipDumpsWhenBatching:  0,
				SkipDumpsInBatch:       0,
				SkipResultsInDump:      0,
			}
			return locations, newCursor, true, nil
		}
	}

	return locations, Cursor{}, false, nil
}

func (s *ReferencePageResolver) handleSameRepoCursor(cursor Cursor) ([]ResolvedLocation, Cursor, bool, error) {
	locations, newCursor, hasNewCursor, err := s.s.performSameRepositoryRemoteReferences(s.repositoryID, s.commit, s.remoteDumpLimit, s.limit, cursor)
	if err != nil {
		return nil, Cursor{}, false, err
	}
	if hasNewCursor {
		return locations, newCursor, true, nil
	}

	newCursor = Cursor{
		DumpID:                 cursor.DumpID,
		Phase:                  "remote-repo",
		Scheme:                 cursor.Scheme,
		Identifier:             cursor.Identifier,
		Name:                   cursor.Name,
		Version:                cursor.Version,
		DumpIDs:                nil,
		TotalDumpsWhenBatching: 0,
		SkipDumpsWhenBatching:  0,
		SkipDumpsInBatch:       0,
		SkipResultsInDump:      0,
	}
	return locations, newCursor, true, nil
}

func (s *ReferencePageResolver) handleRemoteRepoCursor(cursor Cursor) ([]ResolvedLocation, Cursor, bool, error) {
	return s.s.performRemoteReferences(s.repositoryID, s.remoteDumpLimit, s.limit, cursor)
}

//
//
//
//

func (s *Server) performSameDumpReferences(limit int, cursor Cursor) ([]ResolvedLocation, Cursor, bool, error) {
	dump, db, exists, err := s.getDumpAndBundleClient(cursor.DumpID)
	if err != nil {
		return nil, Cursor{}, false, err
	}
	if !exists {
		return nil, Cursor{}, false, nil
	}

	// TODO - make an ordered location set (also in bundle manager)
	locations, err := db.References(cursor.Path, cursor.Line, cursor.Character)
	if err != nil {
		return nil, Cursor{}, false, err
	}

	// Search the references table of the current dump. This search is necessary because
	// we want a 'Find References' operation on a reference to also return references to
	// the governing definition, and those may not be fully linked in the LSIF data. This
	// method returns a cursor if there are reference rows remaining for a subsequent page.
	for _, moniker := range cursor.Monikers {
		results, _, err := db.MonikerResults("reference", moniker.Scheme, moniker.Identifier, nil, nil)
		if err != nil {
			return nil, Cursor{}, false, err
		}

		locations = append(locations, results...)
	}

	// TODO - bounds check
	resolved := resolveLocations(dump, locations[cursor.SkipResults:cursor.SkipResults+limit])

	if newOffset := cursor.SkipResults + limit; newOffset < len(locations) {
		c := Cursor{
			Phase:       cursor.Phase,
			DumpID:      cursor.DumpID,
			Path:        cursor.Path,
			Line:        cursor.Line,
			Character:   cursor.Character,
			Monikers:    cursor.Monikers,
			SkipResults: cursor.SkipResults + limit,
		}

		return resolved, c, true, nil
	}

	return resolved, Cursor{}, false, nil
}

func (s *Server) performDefinitionMonikersReference(limit int, cursor Cursor) ([]ResolvedLocation, Cursor, bool, error) {
	for _, moniker := range cursor.Monikers {
		if moniker.Kind != "import" {
			continue
		}

		locations, count, err := s.lookupMoniker(cursor.DumpID, cursor.Path, moniker, "reference", &limit, &cursor.SkipResults)
		if err != nil {
			return nil, Cursor{}, false, err
		}

		if len(locations) > 0 {
			// TODO - unify these
			resolved, err := s.resolveLocations(locations)
			if err != nil {
				return nil, Cursor{}, false, err
			}

			if newOffset := cursor.SkipResults + len(locations); newOffset < count {
				c := Cursor{
					Phase:       cursor.Phase,
					DumpID:      cursor.DumpID,
					Path:        cursor.Path,
					Monikers:    cursor.Monikers,
					SkipResults: cursor.SkipResults + limit,
				}

				return resolved, c, true, nil
			}

			return resolved, Cursor{}, false, nil
		}
	}

	return nil, Cursor{}, false, nil
}

// TODO - perform transactionally
func (s *Server) getSameRepoRemotePackageReferences(repositoryID int, commit, scheme, name, version, identifier string, limit, offset int) ([]db.Reference, int, int, error) {
	visibleIDs, err := s.db.GetVisibleIDs(repositoryID, commit)
	if err != nil {
		return nil, 0, 0, err
	}

	totalCount, err := s.db.CountSameRepoPackageRefs(scheme, name, version, visibleIDs)
	if err != nil {
		return nil, 0, 0, err
	}

	refs, newOffset, err := s.gatherPackageReferences(identifier, offset, limit, totalCount, func(offset int) ([]db.Reference, error) {
		return s.db.GetSameRepoPackageRefs(scheme, name, version, visibleIDs, offset, limit)
	})
	if err != nil {
		return nil, 0, 0, err
	}

	return refs, totalCount, newOffset, nil
}

// TODO - perform transactionally
func (s *Server) getPackageReferences(repositoryID int, scheme, name, version, identifier string, limit, offset int) ([]db.Reference, int, int, error) {
	totalCount, err := s.db.CountPackageRefs(scheme, name, version, repositoryID)
	if err != nil {
		return nil, 0, 0, err
	}

	refs, newOffset, err := s.gatherPackageReferences(identifier, offset, limit, totalCount, func(offset int) ([]db.Reference, error) {
		return s.db.GetPackageRefs(scheme, name, version, repositoryID, limit, offset)
	})
	if err != nil {
		return nil, 0, 0, err
	}

	return refs, totalCount, newOffset, nil
}

func (s *Server) gatherPackageReferences(identifier string, offset, limit, totalCount int, pager func(offset int) ([]db.Reference, error)) ([]db.Reference, int, error) {
	var refs []db.Reference
	newOffset := offset

	for len(refs) < limit && newOffset < totalCount {
		page, err := pager(newOffset)
		if err != nil {
			return nil, 0, err
		}

		if len(page) == 0 {
			// Shouldn't happen, but just in case of a bug we
			// don't want this to throw up into an infinite loop.
			break
		}

		filtered, scanned := applyBloomFilter(page, identifier, limit-len(refs))
		refs = append(refs, filtered...)
		newOffset += scanned
	}

	return refs, newOffset, nil
}

func (s *Server) performSameRepositoryRemoteReferences(repositoryID int, commit string, remoteDumpLimit, limit int, cursor Cursor) ([]ResolvedLocation, Cursor, bool, error) {
	return s.locationsFromRemoteReferences(cursor.DumpID, cursor.Scheme, cursor.Identifier, limit, cursor, func() ([]db.Reference, int, int, error) {
		return s.getSameRepoRemotePackageReferences(
			repositoryID,
			commit,
			cursor.Scheme,
			cursor.Name,
			cursor.Version,
			cursor.Identifier,
			remoteDumpLimit,
			cursor.SkipDumpsWhenBatching,
		)
	})
}

func (s *Server) performRemoteReferences(repositoryID, remoteDumpLimit, limit int, cursor Cursor) ([]ResolvedLocation, Cursor, bool, error) {
	return s.locationsFromRemoteReferences(cursor.DumpID, cursor.Scheme, cursor.Identifier, limit, cursor, func() ([]db.Reference, int, int, error) {
		return s.getPackageReferences(
			repositoryID,
			cursor.Scheme,
			cursor.Name,
			cursor.Version,
			cursor.Identifier,
			remoteDumpLimit,
			cursor.SkipDumpsWhenBatching,
		)
	})
}

func (s *Server) locationsFromRemoteReferences(dumpID int, scheme, identifier string, limit int, cursor Cursor, fx func() ([]db.Reference, int, int, error)) ([]ResolvedLocation, Cursor, bool, error) {
	if len(cursor.DumpIDs) == 0 {
		packageRefs, newOffset, totalCount, err := fx()
		if err != nil {
			return nil, Cursor{}, false, err
		}

		var dumpIDs []int
		for _, ref := range packageRefs {
			dumpIDs = append(dumpIDs, ref.DumpID)
		}

		cursor.DumpIDs = dumpIDs
		cursor.SkipDumpsWhenBatching = newOffset
		cursor.TotalDumpsWhenBatching = totalCount
	}

	for i, batchDumpID := range cursor.DumpIDs {
		if i < cursor.SkipDumpsInBatch {
			continue
		}

		// Skip the remote reference that show up for ourselves - we've already gathered
		// these in the previous step of the references query.
		if batchDumpID == dumpID {
			continue
		}

		dump, bundleClient, exists, err := s.getDumpAndBundleClient(batchDumpID)
		if err != nil {
			return nil, Cursor{}, false, err
		}
		if !exists {
			continue
		}

		results, count, err := bundleClient.MonikerResults("reference", scheme, identifier, &limit, &cursor.SkipResultsInDump)
		if err != nil {
			return nil, Cursor{}, false, err
		}

		if len(results) > 0 {
			newResultOffset := cursor.SkipResultsInDump + len(results)
			moreDumps := i+1 < len(cursor.DumpIDs)

			// TODO - should be resolved all at once
			resolved := resolveLocations(dump, results)

			var c Cursor
			if newResultOffset < count {
				c = Cursor{
					Phase:                  cursor.Phase,
					DumpID:                 cursor.DumpID,
					Identifier:             cursor.Identifier,
					Scheme:                 cursor.Scheme,
					Name:                   cursor.Name,
					Version:                cursor.Version,
					DumpIDs:                cursor.DumpIDs,
					TotalDumpsWhenBatching: cursor.TotalDumpsWhenBatching,
					SkipDumpsWhenBatching:  cursor.SkipDumpsWhenBatching,
					SkipDumpsInBatch:       cursor.SkipDumpsInBatch,
					SkipResultsInDump:      cursor.SkipResultsInDump + limit,
				}
			} else if moreDumps {
				c = Cursor{
					Phase:                  cursor.Phase,
					DumpID:                 cursor.DumpID,
					Identifier:             cursor.Identifier,
					Scheme:                 cursor.Scheme,
					Name:                   cursor.Name,
					Version:                cursor.Version,
					DumpIDs:                cursor.DumpIDs,
					TotalDumpsWhenBatching: cursor.TotalDumpsWhenBatching,
					SkipDumpsWhenBatching:  cursor.SkipDumpsWhenBatching,
					SkipDumpsInBatch:       i + 1,
					SkipResultsInDump:      0,
				}
			} else if cursor.SkipDumpsWhenBatching < cursor.TotalDumpsWhenBatching {
				c = Cursor{
					Phase:                  cursor.Phase,
					DumpID:                 cursor.DumpID,
					Identifier:             cursor.Identifier,
					Scheme:                 cursor.Scheme,
					Name:                   cursor.Name,
					Version:                cursor.Version,
					TotalDumpsWhenBatching: cursor.TotalDumpsWhenBatching,
					SkipDumpsWhenBatching:  cursor.SkipDumpsWhenBatching,
					DumpIDs:                []int{},
					SkipDumpsInBatch:       0,
					SkipResultsInDump:      0,
				}
			} else {
				return resolved, c, false, nil
			}

			return resolved, c, true, nil
		}
	}

	return nil, Cursor{}, false, nil
}
