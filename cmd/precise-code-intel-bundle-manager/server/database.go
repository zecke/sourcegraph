package server

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"sort"
	"strconv"

	"github.com/jmoiron/sqlx"
	"github.com/keegancsmith/sqlf"
)

type Database struct {
	db *sqlx.DB
}

type ID string

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

func NewDatabase(filename string) *Database {
	db, err := sqlx.Open("sqlite3_with_pcre", filename)
	if err != nil {
		panic(err.Error())
	}

	return &Database{
		db: db,
	}
}

func (db *Database) Close() error {
	return db.db.Close()
}

func (db *Database) Exists(path string) (bool, error) {
	_, exists, err := db.getDocumentData(path)
	return exists, err
}

func (db *Database) Definitions(path string, line, character int) ([]InternalLocation, error) {
	documentData, ranges, exists, err := db.getRangeByPosition(path, line, character)
	if err != nil || !exists {
		return nil, err
	}

	for _, r := range ranges {
		if r.DefinitionResultID == "" {
			continue
		}

		definitionResults, err := db.getResultByID(r.DefinitionResultID)
		if err != nil {
			return nil, err
		}

		return db.convertRangesToInternalLocations(documentData, definitionResults)
	}

	return nil, nil
}

func (db *Database) References(path string, line, character int) ([]InternalLocation, error) {
	documentData, ranges, exists, err := db.getRangeByPosition(path, line, character)
	if err != nil || !exists {
		return nil, err
	}

	var allLocations []InternalLocation
	for _, r := range ranges {
		if r.ReferenceResultID == "" {
			continue
		}

		referenceResults, err := db.getResultByID(r.ReferenceResultID)
		if err != nil {
			return nil, err
		}

		locations, err := db.convertRangesToInternalLocations(documentData, referenceResults)
		if err != nil {
			return nil, err
		}

		allLocations = append(allLocations, locations...)
	}

	return allLocations, nil
}

func (db *Database) Hover(path string, line, character int) (string, Range, bool, error) {
	documentData, ranges, exists, err := db.getRangeByPosition(path, line, character)
	if err != nil || !exists {
		return "", Range{}, false, err
	}

	for _, r := range ranges {
		if r.HoverResultID == "" {
			continue
		}

		text, exists := documentData.HoverResults[r.HoverResultID]
		if !exists {
			return "", Range{}, false, fmt.Errorf("unknown hover result %s", r.HoverResultID)
		}

		return text, toRange(r.StartLine, r.StartCharacter, r.EndLine, r.EndCharacter), true, nil
	}

	return "", Range{}, false, nil
}

func (db *Database) MonikerByPosition(path string, line, character int) ([][]MonikerData, error) {
	documentData, ranges, exists, err := db.getRangeByPosition(path, line, character)
	if err != nil || !exists {
		return nil, err
	}

	var monikerData [][]MonikerData
	for _, r := range ranges {
		var batch []MonikerData
		for _, monikerID := range r.MonikerIDs {
			moniker, exists := documentData.Monikers[monikerID]
			if !exists {
				return nil, fmt.Errorf("unknown moniker %s", monikerID)
			}

			batch = append(batch, moniker)
		}

		monikerData = append(monikerData, batch)
	}

	return monikerData, nil
}

func (db *Database) MonikerResults(tableName, scheme, identifier string, skip, take int) ([]InternalLocation, int, error) {

	query := sqlf.Sprintf("SELECT * FROM %s WHERE scheme = :scheme AND identifier := identifier LIMIT %s OFFSET %s", tableName, scheme, identifier, take, skip)

	var rows []struct {
		ID             int    `db:"id"`
		Scheme         string `db:"scheme"`
		Identifier     string `db:"identifier"`
		DocumentPath   string `db:"documentPath"`
		StartLine      int    `db:"startLine"`
		EndLine        int    `db:"endLine"`
		StartCharacter int    `db:"startCharacter"`
		EndCharacter   int    `db:"endCharacter"`
	}

	if err := db.db.Select(&rows, query.Query(sqlf.PostgresBindVar), query.Args()); err != nil {
		return nil, 0, err
	}

	var locations []InternalLocation
	for _, row := range rows {
		locations = append(locations, InternalLocation{
			Path:  row.DocumentPath,
			Range: toRange(row.StartLine, row.StartCharacter, row.EndLine, row.EndCharacter),
		})
	}

	countQuery := sqlf.Sprintf("SELECT COUNT(1) FROM %s WHERE scheme = :scheme AND identifier := identifier", tableName, scheme, identifier)

	var totalCount int
	if err := db.db.Get(&totalCount, countQuery.Query(sqlf.PostgresBindVar), countQuery.Args()); err != nil {
		return nil, 0, err
	}

	return locations, totalCount, nil
}

func (db *Database) PackageInformation(path string, packageInformationID ID) (PackageInformationData, bool, error) {
	documentData, exists, err := db.getDocumentData(path)
	if err != nil {
		return PackageInformationData{}, false, err
	}

	if !exists {
		return PackageInformationData{}, false, nil
	}

	packageInformationData, exists := documentData.PackageInformation[packageInformationID]
	return packageInformationData, exists, nil
}

//
//

func (db *Database) getDocumentData(path string) (DocumentData, bool, error) {
	var data string
	if err := db.db.Get(&data, "SELECT data FROM documents WHERE path = :path", path); err != nil {
		if err == sql.ErrNoRows {
			return DocumentData{}, false, nil
		}

		return DocumentData{}, false, err
	}

	// TODO - cache result
	documentData, err := unmarshalDocumentData([]byte(data))
	return documentData, true, err
}

//
//

func (db *Database) getRangeByPosition(path string, line, character int) (DocumentData, []RangeData, bool, error) {
	documentData, exists, err := db.getDocumentData(path)
	if err != nil {
		return DocumentData{}, nil, false, err
	}

	if !exists {
		return DocumentData{}, nil, false, nil
	}

	return documentData, findRanges(documentData.Ranges, line, character), true, nil
}

func findRanges(ranges map[ID]RangeData, line, character int) []RangeData {
	var filtered []RangeData
	for _, r := range ranges {
		if comparePosition(r, line, character) == 0 {
			filtered = append(filtered, r)
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		return comparePosition(filtered[i], filtered[j].StartLine, filtered[j].StartCharacter) == 0
	})

	return filtered
}

func comparePosition(r RangeData, line, character int) int {
	if line < r.StartLine {
		return 1
	}

	if line > r.EndLine {
		return -1
	}

	if line == r.StartLine && character < r.StartCharacter {
		return 1
	}

	if line == r.EndLine && character > r.EndCharacter {
		return -1
	}

	return 0
}

//
//

func (db *Database) getResultByID(id ID) ([]DocumentPathRangeID, error) {
	resultChunkData, exists, err := db.getResultChunkByResultID(id)
	if err != nil {
		return nil, err
	}

	if !exists {
		// TODO - make a richer error type here
		return nil, fmt.Errorf("unknown result chunk %s", id)
	}

	documentIDRangeIDs, exists := resultChunkData.DocumentIDRangeIDs[id]
	if !exists {
		return nil, fmt.Errorf("unknown result %s", id)
	}

	var resultData []DocumentPathRangeID
	for _, documentIDRangeID := range documentIDRangeIDs {
		path, ok := resultChunkData.DocumentPaths[documentIDRangeID.DocumentID]
		if !ok {
			return nil, fmt.Errorf("unknown document path %s", documentIDRangeID.DocumentID)
		}

		resultData = append(resultData, DocumentPathRangeID{
			Path:    path,
			RangeID: documentIDRangeID.RangeID,
		})
	}

	return resultData, nil
}

func (db *Database) getResultChunkByResultID(id ID) (ResultChunkData, bool, error) {
	numResultChunks, err := db.getNumResultChunks()
	if err != nil {
		return ResultChunkData{}, false, err
	}

	var data string
	if err := db.db.Get(&data, "SELECT data FROM resultChunks WHERE id = :id", hashKey(id, numResultChunks)); err != nil {
		if err == sql.ErrNoRows {
			return ResultChunkData{}, false, nil
		}

		return ResultChunkData{}, false, err
	}

	// TODO - cache result
	resultChunkData, err := unmarshalResultChunkData([]byte(data))
	return resultChunkData, true, err
}

func (db *Database) getNumResultChunks() (int, error) {
	var numResultChunks int
	if err := db.db.Get(&numResultChunks, "SELECT numResultChunks FROM meta WHERE id = 1"); err != nil {
		return 0, err
	}

	// TODO - cache result
	return numResultChunks, nil
}

func hashKey(id ID, maxIndex int) int {
	hash := 0
	for _, c := range string(id) {
		chr := int(c)
		hash = (hash << 5) - hash + chr
		hash |= 0
	}

	if hash < 0 {
		hash = -hash
	}
	return hash % maxIndex
}

//
//

func (db *Database) convertRangesToInternalLocations(document DocumentData, resultData []DocumentPathRangeID) ([]InternalLocation, error) {
	var locations []InternalLocation
	for _, documentPathRangeID := range resultData {
		// TODO - deduplicate requests for documents
		documentData, exists, err := db.getDocumentData(documentPathRangeID.Path)
		if err != nil {
			return nil, err
		}

		if !exists {
			return nil, fmt.Errorf("unknown document %s", documentPathRangeID.Path)
		}

		r, exists := documentData.Ranges[documentPathRangeID.RangeID]
		if !exists {
			return nil, fmt.Errorf("unknown range %s", documentPathRangeID.RangeID)
		}

		locations = append(locations, InternalLocation{
			Path:  documentPathRangeID.Path,
			Range: toRange(r.StartLine, r.StartCharacter, r.EndLine, r.EndCharacter),
		})
	}

	return locations, nil
}

//
//

type wrappedMapValue struct {
	Value []json.RawMessage `json:"value"`
}

type wrappedSetValue struct {
	Value []json.RawMessage `json:"value"`
}

func unmarshalDocumentData(data []byte) (DocumentData, error) {
	payload := struct {
		Ranges             wrappedMapValue `json:"ranges"`
		HoverResults       wrappedMapValue `json:"hoverResults"`
		Monikers           wrappedMapValue `json:"monikers"`
		PackageInformation wrappedMapValue `json:"packageInformation"`
	}{}

	if err := unmarshalGzippedJSON(data, &payload); err != nil {
		return DocumentData{}, err
	}

	ranges, err := unmarshalWrappedRanges(payload.Ranges.Value)
	if err != nil {
		return DocumentData{}, err
	}

	hoverResults, err := unmarshalWrappedHoverResults(payload.HoverResults.Value)
	if err != nil {
		return DocumentData{}, err
	}

	monikers, err := unmarshalWrappedMonikers(payload.Monikers.Value)
	if err != nil {
		return DocumentData{}, err
	}

	packageInformation, err := unmarshalWrappedPackageInformation(payload.PackageInformation.Value)
	if err != nil {
		return DocumentData{}, err
	}

	return DocumentData{
		Ranges:             ranges,
		HoverResults:       hoverResults,
		Monikers:           monikers,
		PackageInformation: packageInformation,
	}, nil
}

func unmarshalWrappedRanges(pairs []json.RawMessage) (map[ID]RangeData, error) {
	m := map[ID]RangeData{}
	for _, pair := range pairs {
		var id ID
		var value struct {
			StartLine          int             `json:"startLine"`
			StartCharacter     int             `json:"startCharacter"`
			EndLine            int             `json:"endLine"`
			EndCharacter       int             `json:"endCharacter"`
			DefinitionResultID ID              `json:"definitionResultID"`
			ReferenceResultID  ID              `json:"referenceResultID"`
			HoverResultID      ID              `json:"hoverResultID"`
			MonikerIDs         wrappedSetValue `json:"monikerIDs"`
		}

		target := []interface{}{&id, &value}
		if err := json.Unmarshal([]byte(pair), &target); err != nil {
			return nil, err
		}

		var monikerIDs []ID
		for _, value := range value.MonikerIDs.Value {
			var id ID
			if err := json.Unmarshal([]byte(value), &id); err != nil {
				return nil, err
			}

			monikerIDs = append(monikerIDs, id)
		}

		m[id] = RangeData{
			StartLine:          value.StartLine,
			StartCharacter:     value.StartCharacter,
			EndLine:            value.EndLine,
			EndCharacter:       value.EndCharacter,
			DefinitionResultID: value.DefinitionResultID,
			ReferenceResultID:  value.ReferenceResultID,
			HoverResultID:      value.HoverResultID,
			MonikerIDs:         monikerIDs,
		}
	}

	return m, nil
}

func unmarshalWrappedHoverResults(pairs []json.RawMessage) (map[ID]string, error) {
	m := map[ID]string{}
	for _, pair := range pairs {
		var id ID
		var value string

		target := []interface{}{&id, &value}
		if err := json.Unmarshal([]byte(pair), &target); err != nil {
			return nil, err
		}

		m[id] = value
	}

	return m, nil
}

func unmarshalWrappedMonikers(pairs []json.RawMessage) (map[ID]MonikerData, error) {
	m := map[ID]MonikerData{}
	for _, pair := range pairs {
		var id ID
		var value struct {
			Kind                 string `json:"kind"`
			Scheme               string `json:"scheme"`
			Identifier           string `json:"identifier"`
			PackageInformationID ID     `json:"packageInformationID"`
		}

		target := []interface{}{&id, &value}
		if err := json.Unmarshal([]byte(pair), &target); err != nil {
			return nil, err
		}

		m[id] = MonikerData{
			Kind:                 value.Kind,
			Scheme:               value.Scheme,
			Identifier:           value.Identifier,
			PackageInformationID: value.PackageInformationID,
		}
	}

	return m, nil
}

func unmarshalWrappedPackageInformation(pairs []json.RawMessage) (map[ID]PackageInformationData, error) {
	m := map[ID]PackageInformationData{}
	for _, pair := range pairs {
		var id ID
		var value struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}

		target := []interface{}{&id, &value}
		if err := json.Unmarshal([]byte(pair), &target); err != nil {
			return nil, err
		}

		m[id] = PackageInformationData{
			Name:    value.Name,
			Version: value.Version,
		}
	}

	return m, nil
}

func unmarshalResultChunkData(data []byte) (ResultChunkData, error) {
	payload := struct {
		DocumentPaths      wrappedMapValue `json:"documentPaths"`
		DocumentIDRangeIDs wrappedMapValue `json:"documentIdRangeIds"`
	}{}

	if err := unmarshalGzippedJSON(data, &payload); err != nil {
		return ResultChunkData{}, err
	}

	documentPaths, err := unmarshalWrappedDocumentPaths(payload.DocumentPaths.Value)
	if err != nil {
		return ResultChunkData{}, err
	}

	documentIDRangeIDs, err := unmarshalWrappedDocumentIdRangeIDs(payload.DocumentIDRangeIDs.Value)
	if err != nil {
		return ResultChunkData{}, err
	}

	return ResultChunkData{
		DocumentPaths:      documentPaths,
		DocumentIDRangeIDs: documentIDRangeIDs,
	}, nil
}

func unmarshalWrappedDocumentPaths(pairs []json.RawMessage) (map[ID]string, error) {
	m := map[ID]string{}
	for _, pair := range pairs {
		var id ID
		var value string

		target := []interface{}{&id, &value}
		if err := json.Unmarshal([]byte(pair), &target); err != nil {
			return nil, err
		}

		m[id] = value
	}

	return m, nil
}

func unmarshalWrappedDocumentIdRangeIDs(pairs []json.RawMessage) (map[ID][]DocumentIDRangeID, error) {
	m := map[ID][]DocumentIDRangeID{}
	for _, pair := range pairs {
		var id ID
		var value []struct {
			DocumentID ID `json:"documentId"`
			RangeID    ID `json:"rangeId"`
		}

		target := []interface{}{&id, &value}
		if err := json.Unmarshal([]byte(pair), &target); err != nil {
			return nil, err
		}

		var documentIDRangeIDs []DocumentIDRangeID
		for _, v := range value {
			documentIDRangeIDs = append(documentIDRangeIDs, DocumentIDRangeID{
				DocumentID: v.DocumentID,
				RangeID:    v.RangeID,
			})
		}

		m[id] = documentIDRangeIDs
	}

	return m, nil
}

func unmarshalGzippedJSON(data []byte, payload interface{}) error {
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}

	decompressed, err := ioutil.ReadAll(gzipReader)
	if err != nil {
		return err
	}

	return json.Unmarshal(decompressed, &payload)
}

func toRange(startLine, startCharacter, endLine, endCharacter int) Range {
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
