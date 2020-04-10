package server

import (
	"github.com/sourcegraph/sourcegraph/cmd/precise-code-intel-api-server/server/bundles"
	"github.com/sourcegraph/sourcegraph/cmd/precise-code-intel-api-server/server/db"
)

type ResolvedLocation struct {
	Dump  db.Dump       `json:"dump"`
	Path  string        `json:"path"`
	Range bundles.Range `json:"range"`
}

type APILocation struct {
	RepositoryID int           `json:"repositoryId"`
	Commit       string        `json:"commit"`
	Path         string        `json:"path"`
	Range        bundles.Range `json:"range"`
}

func resolveLocations(dump db.Dump, locations []bundles.Location) []ResolvedLocation {
	var resolvedLocations []ResolvedLocation
	for _, location := range locations {
		resolvedLocations = append(resolvedLocations, ResolvedLocation{
			Dump:  dump,
			Path:  dump.Root + location.Path,
			Range: location.Range,
		})
	}

	return resolvedLocations
}

func (s *Server) resolveLocations(locations []bundles.Location) ([]ResolvedLocation, error) {
	uniq := map[int]struct{}{}
	for _, l := range locations {
		uniq[l.DumpID] = struct{}{}
	}

	var ids []int
	for k := range uniq {
		ids = append(ids, k)
	}

	dumpsByID, err := s.db.GetDumps(ids)
	if err != nil {
		return nil, err
	}

	var thingers []ResolvedLocation
	for _, location := range locations {
		thingers = append(thingers, ResolvedLocation{
			Dump:  dumpsByID[location.DumpID],
			Path:  dumpsByID[location.DumpID].Root + location.Path,
			Range: location.Range,
		})
	}

	return thingers, nil
}

func (s *Server) serializeLocations(resolvedLocations []ResolvedLocation) ([]APILocation, error) {
	var apiLocations []APILocation
	for _, res := range resolvedLocations {
		apiLocations = append(apiLocations, APILocation{
			RepositoryID: res.Dump.RepositoryID,
			Commit:       res.Dump.Commit,
			Path:         res.Path,
			Range:        res.Range,
		})
	}

	return apiLocations, nil
}
