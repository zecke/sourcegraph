package server

import (
	"fmt"

	"github.com/sourcegraph/sourcegraph/cmd/precise-code-intel-api-server/server/db"
)

type PaginatedLocations struct {
	locations []LocationThingers
	newCursor *Cursor // TODO - no pointers
}

func (s *Server) handlePagination(repositoryID int, commit string, remoteDumpLimit, limit int, cursor Cursor) (PaginatedLocations, error) {
	recur := func(
		handler func() (PaginatedLocations, error),
		makeCursor func() *Cursor, // TODO - no pointers
	) (PaginatedLocations, error) {
		plocs, err := handler()
		if err != nil {
			return PaginatedLocations{}, err
		}

		if plocs.newCursor == nil {
			plocs.newCursor = makeCursor()
		}

		if plocs.newCursor == nil {
			return plocs, nil
		}

		limit -= len(plocs.locations)
		if limit <= 0 {
			return plocs, nil
		}

		plocs2, err := s.handlePagination(repositoryID, commit, remoteDumpLimit, limit, *plocs.newCursor)
		if err != nil {
			return PaginatedLocations{}, err
		}

		return PaginatedLocations{
			locations: append(plocs.locations, plocs2.locations...),
			newCursor: plocs2.newCursor,
		}, nil
	}

	switch cursor.Phase {
	case "same-dump":
		return recur(
			func() (PaginatedLocations, error) {
				return s.performSameDumpReferences(limit, cursor)
			},
			func() *Cursor {
				return &Cursor{
					DumpID:      cursor.DumpID,
					Phase:       "definition-monikers",
					Path:        cursor.Path,
					Line:        cursor.Line,
					Character:   cursor.Character,
					Monikers:    cursor.Monikers,
					SkipResults: 0,
				}
			},
		)
	case "definition-monikers":
		return recur(
			func() (PaginatedLocations, error) {
				return s.performDefinitionMonikersReference(limit, cursor)
			},
			func() *Cursor {
				for _, moniker := range cursor.Monikers {
					packageInformation, exists, err := s.lookupPackageInformation(cursor.DumpID, cursor.Path, moniker)
					if err != nil {
						// TODO - implement
					}
					if !exists {
						// TODO - oops lop is wrong
						// continue
						return nil
					}

					return &Cursor{
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
				}
				return nil
			},
		)
	case "same-repo":
		return recur(
			func() (PaginatedLocations, error) {
				return s.performSameRepositoryRemoteReferences(repositoryID, commit, remoteDumpLimit, limit, cursor)
			},
			func() *Cursor {
				return &Cursor{
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
			},
		)
	case "remote-repo":
		return recur(
			func() (PaginatedLocations, error) {
				return s.performRemoteReferences(repositoryID, remoteDumpLimit, limit, cursor)
			},
			func() *Cursor {
				return nil
			},
		)
	}

	return PaginatedLocations{}, fmt.Errorf("malformed cursor")
}

func (s *Server) performSameDumpReferences(limit int, cursor Cursor) (PaginatedLocations, error) {
	dump, db, exists, err := s.getDumpAndDatabase(cursor.DumpID)
	if err != nil {
		return PaginatedLocations{}, err
	}
	if !exists {
		return PaginatedLocations{}, nil
	}

	// TODO - make an ordered location set (also in bundle manager)
	locations, err := db.References(cursor.Path, cursor.Line, cursor.Character)
	if err != nil {
		return PaginatedLocations{}, err
	}

	// Search the references table of the current dump. This search is necessary because
	// we want a 'Finddb.References' operation on a reference to also return references to
	// the governing definition, and those may not be fully linked in the LSIF data. This
	// method returns a cursor if there are reference rows remaining for a subsequent page.
	for _, moniker := range cursor.Monikers {
		results, _, err := db.MonikerResults("reference", moniker.Scheme, moniker.Identifier, nil, nil)
		if err != nil {
			return PaginatedLocations{}, err
		}

		locations = append(locations, results...)
	}

	pl := PaginatedLocations{
		// TODO - bounds check
		locations: s.resolveLocations(dump.Root, locations[cursor.SkipResults:cursor.SkipResults+limit]),
	}

	if newOffset := cursor.SkipResults + limit; newOffset < len(locations) {
		pl.newCursor = &Cursor{
			Phase:       cursor.Phase,
			DumpID:      cursor.DumpID,
			Path:        cursor.Path,
			Line:        cursor.Line,
			Character:   cursor.Character,
			Monikers:    cursor.Monikers,
			SkipResults: cursor.SkipResults + limit,
		}
	}

	return pl, nil
}

func (s *Server) performDefinitionMonikersReference(limit int, cursor Cursor) (PaginatedLocations, error) {
	for _, moniker := range cursor.Monikers {
		if moniker.Kind != "import" {
			continue
		}

		locations, count, err := s.lookupMoniker(cursor.DumpID, cursor.Path, moniker, "reference", &limit, &cursor.SkipResults)
		if err != nil {
			return PaginatedLocations{}, err
		}

		if len(locations) > 0 {
			pl := PaginatedLocations{
				//db.Dump already appended by lookupMoniker
				locations: s.resolveLocations("", locations),
			}

			if newOffset := cursor.SkipResults + len(locations); newOffset < count {
				pl.newCursor = &Cursor{
					Phase:       cursor.Phase,
					DumpID:      cursor.DumpID,
					Path:        cursor.Path,
					Monikers:    cursor.Monikers,
					SkipResults: cursor.SkipResults + limit,
				}
			}

			return pl, nil
		}
	}

	return PaginatedLocations{}, nil
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

func applyBloomFilter(refs []db.Reference, identifier string, limit int) ([]db.Reference, int) {
	return refs, len(refs) // TODO - implement
}

func (s *Server) performSameRepositoryRemoteReferences(repositoryID int, commit string, remoteDumpLimit, limit int, cursor Cursor) (PaginatedLocations, error) {
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

func (s *Server) performRemoteReferences(repositoryID, remoteDumpLimit, limit int, cursor Cursor) (PaginatedLocations, error) {
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

func (s *Server) locationsFromRemoteReferences(dumpID int, scheme, identifier string, limit int, cursor Cursor, fx func() ([]db.Reference, int, int, error)) (PaginatedLocations, error) {
	if len(cursor.DumpIDs) == 0 {
		packageRefs, newOffset, totalCount, err := fx()
		if err != nil {
			return PaginatedLocations{}, err
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

		dump, db, exists, err := s.getDumpAndDatabase(batchDumpID)
		if err != nil {
			return PaginatedLocations{}, err
		}
		if !exists {
			continue
		}

		results, count, err := db.MonikerResults("reference", scheme, identifier, &limit, &cursor.SkipResultsInDump)
		if err != nil {
			return PaginatedLocations{}, err
		}

		if len(results) > 0 {
			newResultOffset := cursor.SkipResultsInDump + len(results)
			moreDumps := i+1 < len(cursor.DumpIDs)

			pl := PaginatedLocations{
				locations: s.resolveLocations(dump.Root, results),
			}

			if newResultOffset < count {
				pl.newCursor = &Cursor{
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
				pl.newCursor = &Cursor{
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
				pl.newCursor = &Cursor{
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
			}

			return pl, nil
		}
	}

	return PaginatedLocations{}, nil
}
