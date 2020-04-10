package server

type LocationThingers struct {
	DumpID int    `json:"dumpId"`
	Path   string `json:"path"`
	Range  Range  `json:"Range"`
}

type LocationThingers2 struct {
	Dump  Dump   `json:"dump"`
	Path  string `json:"path"`
	Range Range  `json:"range"`
}

type Outer struct {
	RepositoryID int    `json:"repositoryId"`
	Commit       string `json:"commit"`
	Path         string `json:"path"`
	Range        Range  `json:"range"`
}

func (s *Server) resolveLocations(root string, locations []Location) []LocationThingers {
	var thingers []LocationThingers
	for _, location := range locations {
		thingers = append(thingers, LocationThingers{
			DumpID: location.DumpID,
			Path:   root + location.Path,
			Range:  location.Range,
		})
	}

	return thingers
}

func (s *Server) resolveLocations2(locations []LocationThingers) ([]LocationThingers2, error) {
	uniq := map[int]struct{}{}
	for _, l := range locations {
		uniq[l.DumpID] = struct{}{}
	}

	var ids []int
	for k := range uniq {
		ids = append(ids, k)
	}

	dumpsByID, err := s.getDumps(ids)
	if err != nil {
		return nil, err
	}

	var thingers []LocationThingers2
	for _, location := range locations {
		thingers = append(thingers, LocationThingers2{
			Dump:  dumpsByID[location.DumpID],
			Path:  location.Path,
			Range: location.Range,
		})
	}

	return thingers, nil
}

func (s *Server) serializeLocations(locations []LocationThingers) ([]Outer, error) {
	resolved, err := s.resolveLocations2(locations)
	if err != nil {
		return nil, err
	}

	var outers []Outer
	for _, res := range resolved {
		outers = append(outers, Outer{
			RepositoryID: res.Dump.RepositoryID,
			Commit:       res.Dump.Commit,
			Path:         res.Path,
			Range:        res.Range,
		})
	}

	return outers, nil
}
