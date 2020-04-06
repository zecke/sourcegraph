package server

import (
	"database/sql"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/keegancsmith/sqlf"
)

type Database struct {
	filename             string
	documentDataCache    *DocumentDataCache
	resultChunkDataCache *ResultChunkDataCache
	db                   *sqlx.DB
	numResultChunks      int
}

type ErrMalformedBundle struct {
	Filename string
	Name     string
	Key      string
}

func (e ErrMalformedBundle) Error() string {
	return fmt.Sprintf("malformed bundle: unknown %s %s", e.Name, e.Key)
}

func OpenDatabase(filename string, documentDataCache *DocumentDataCache, resultChunkDataCache *ResultChunkDataCache) (*Database, error) {
	db, err := sqlx.Open("sqlite3_with_pcre", filename)
	if err != nil {
		return nil, err
	}

	var numResultChunks int
	if err := db.Get(&numResultChunks, "SELECT numResultChunks FROM meta LIMIT 1"); err != nil {
		return nil, err
	}

	return &Database{
		db:                   db,
		documentDataCache:    documentDataCache,
		resultChunkDataCache: resultChunkDataCache,
		numResultChunks:      numResultChunks,
	}, nil
}

func (db *Database) Close() error {
	return db.db.Close()
}

func (db *Database) Exists(path string) (bool, error) {
	_, exists, err := db.getDocumentData(path)
	return exists, err
}

func (db *Database) Definitions(path string, line, character int) ([]InternalLocation, error) {
	_, ranges, exists, err := db.getRangeByPosition(path, line, character)
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

		return db.convertRangesToInternalLocations(definitionResults)
	}

	return nil, nil
}

func (db *Database) References(path string, line, character int) ([]InternalLocation, error) {
	_, ranges, exists, err := db.getRangeByPosition(path, line, character)
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

		locations, err := db.convertRangesToInternalLocations(referenceResults)
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
			return "", Range{}, false, ErrMalformedBundle{
				Filename: db.filename,
				Name:     "hoverResult",
				Key:      string(r.HoverResultID),
			}
		}

		return text, newRange(r.StartLine, r.StartCharacter, r.EndLine, r.EndCharacter), true, nil
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
				return nil, ErrMalformedBundle{
					Filename: db.filename,
					Name:     "moniker",
					Key:      string(monikerID),
				}
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
			Range: newRange(row.StartLine, row.StartCharacter, row.EndLine, row.EndCharacter),
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

func (db *Database) getDocumentData(path string) (DocumentData, bool, error) {
	documentData, err := db.documentDataCache.Get(fmt.Sprintf("%s::%s", db.filename, path), func() (DocumentData, error) {
		var data string
		if err := db.db.Get(&data, "SELECT data FROM documents WHERE path = :path", path); err != nil {
			return DocumentData{}, err
		}

		return unmarshalDocumentData([]byte(data))
	})

	if err != nil {
		if err == sql.ErrNoRows {
			return DocumentData{}, false, nil
		}

		return DocumentData{}, false, err
	}

	return documentData, true, err
}

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

func (db *Database) getResultByID(id ID) ([]DocumentPathRangeID, error) {
	resultChunkData, exists, err := db.getResultChunkByResultID(id)
	if err != nil {
		return nil, err
	}

	if !exists {
		return nil, ErrMalformedBundle{
			Filename: db.filename,
			Name:     "result chunk",
			Key:      string(id),
		}
	}

	documentIDRangeIDs, exists := resultChunkData.DocumentIDRangeIDs[id]
	if !exists {
		return nil, ErrMalformedBundle{
			Filename: db.filename,
			Name:     "result",
			Key:      string(id),
		}
	}

	var resultData []DocumentPathRangeID
	for _, documentIDRangeID := range documentIDRangeIDs {
		path, ok := resultChunkData.DocumentPaths[documentIDRangeID.DocumentID]
		if !ok {
			return nil, ErrMalformedBundle{
				Filename: db.filename,
				Name:     "documentPath",
				Key:      string(documentIDRangeID.DocumentID),
			}
		}

		resultData = append(resultData, DocumentPathRangeID{
			Path:    path,
			RangeID: documentIDRangeID.RangeID,
		})
	}

	return resultData, nil
}

func (db *Database) getResultChunkByResultID(id ID) (ResultChunkData, bool, error) {
	resultChunkData, err := db.resultChunkDataCache.Get(fmt.Sprintf("%s::%s", db.filename, id), func() (ResultChunkData, error) {
		var data string
		if err := db.db.Get(&data, "SELECT data FROM resultChunks WHERE id = :id", hashKey(id, db.numResultChunks)); err != nil {
			return ResultChunkData{}, err
		}

		return unmarshalResultChunkData([]byte(data))
	})

	if err != nil {
		if err == sql.ErrNoRows {
			return ResultChunkData{}, false, nil
		}

		return ResultChunkData{}, false, err
	}

	return resultChunkData, true, err
}

func (db *Database) convertRangesToInternalLocations(resultData []DocumentPathRangeID) ([]InternalLocation, error) {
	groupedResults := map[string][]ID{}
	for _, documentPathRangeID := range resultData {
		groupedResults[documentPathRangeID.Path] = append(groupedResults[documentPathRangeID.Path], documentPathRangeID.RangeID)
	}

	var locations []InternalLocation
	for path, rangeIDs := range groupedResults {
		documentData, exists, err := db.getDocumentData(path)
		if err != nil {
			return nil, err
		}

		if !exists {
			return nil, ErrMalformedBundle{
				Filename: db.filename,
				Name:     "document",
				Key:      string(path),
			}
		}

		for _, rangeID := range rangeIDs {
			r, exists := documentData.Ranges[rangeID]
			if !exists {
				return nil, ErrMalformedBundle{
					Filename: db.filename,
					Name:     "range",
					Key:      string(rangeID),
				}
			}

			locations = append(locations, InternalLocation{
				Path:  path,
				Range: newRange(r.StartLine, r.StartCharacter, r.EndLine, r.EndCharacter),
			})
		}
	}

	return locations, nil
}
