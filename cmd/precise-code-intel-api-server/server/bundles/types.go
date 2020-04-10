package bundles

type Location struct {
	DumpID int    `json:"dumpId"`
	Path   string `json:"path"`
	Range  Range  `json:"range"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type MonikerData struct {
	Kind                 string `json:"kind"`
	Scheme               string `json:"scheme"`
	Identifier           string `json:"identifier"`
	PackageInformationID string `json:"packageInformationID"`
}

type PackageInformationData struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
