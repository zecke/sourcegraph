package server

import (
	"encoding/json"
	"strconv"
)

type ID string

type DocumentData struct {
	Ranges             map[ID]RangeData
	HoverResults       map[ID]string
	Monikers           map[ID]MonikerData
	PackageInformation map[ID]PackageInformationData
}

type RangeData struct {
	StartLine          int
	StartCharacter     int
	EndLine            int
	EndCharacter       int
	DefinitionResultID ID
	ReferenceResultID  ID
	HoverResultID      ID
	MonikerIDs         []ID
}

type MonikerData struct {
	Kind                 string `json:"kind"`
	Scheme               string `json:"scheme"`
	Identifier           string `json:"identifier"`
	PackageInformationID ID     `json:"packageInformationId"`
}

type PackageInformationData struct {
	Name    string
	Version string
}

type ResultChunkData struct {
	DocumentPaths      map[ID]string
	DocumentIDRangeIDs map[ID][]DocumentIDRangeID
}

type DocumentIDRangeID struct {
	DocumentID ID
	RangeID    ID
}

type DocumentPathRangeID struct {
	Path    string
	RangeID ID
}

type InternalLocation struct {
	Path  string `json:"path"`
	Range Range  `json:"range"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

func (id *ID) UnmarshalJSON(b []byte) error {
	if b[0] == '"' {
		return json.Unmarshal(b, (*string)(id))
	}

	var value int64
	if err := json.Unmarshal(b, &value); err != nil {
		return err
	}

	*id = ID(strconv.FormatInt(value, 10))
	return nil
}

func newRange(startLine, startCharacter, endLine, endCharacter int) Range {
	return Range{
		Start: Position{
			Line:      startLine,
			Character: startCharacter,
		},
		End: Position{
			Line:      endLine,
			Character: endCharacter,
		},
	}
}
