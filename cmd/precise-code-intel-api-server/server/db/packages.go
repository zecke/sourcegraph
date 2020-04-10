package db

import (
	"context"
	"database/sql"

	"github.com/keegancsmith/sqlf"
)

type Reference struct {
	DumpID int
	Filter string
}

func (db *DB) GetVisibleIDs(repositoryID int, commit string) ([]int, error) {
	rows, err := db.db.QueryContext(context.Background(), "WITH "+bidirectionalLineage+", "+visibleDumps+"SELECT id FROM visible_ids", repositoryID, commit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var visibleIDs []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		visibleIDs = append(visibleIDs, id)
	}

	return visibleIDs, nil
}

func (db *DB) CountPackageRefs(scheme, name, version string, repositoryID int) (int, error) {
	query := `
		SELECT COUNT(1) FROM lsif_references r
		LEFT JOIN lsif_dumps d ON r.dump_id = d.id
		WHERE scheme = $1 AND name = $2 AND version = $3 AND d.repository_id != $4 AND d.visible_at_tip = true
	`

	var totalCount int
	if err := db.db.QueryRowContext(context.Background(), query, scheme, name, version, repositoryID).Scan(&totalCount); err != nil {
		return 0, err
	}

	return totalCount, nil
}

func (db *DB) GetPackageRefs(scheme, name, version string, repositoryID, limit, offset int) ([]Reference, error) {
	queryx := `
			SELECT d.id, r.filter FROM lsif_references r
			LEFT JOIN lsif_dumps d ON r.dump_id = d.id
			WHERE scheme = $1 AND name = $2 AND version = $3 AND d.repository_id != $4 AND d.visible_at_tip = true
			ORDER BY d.repository_id, d.root
			LIMIT $5
			OFFSET $6
		`

	rows, err := db.db.QueryContext(context.Background(), queryx, scheme, name, version, repositoryID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []Reference
	for rows.Next() {
		var dumpID int
		var filter string

		if err := rows.Scan(&dumpID, &filter); err != nil {
			return nil, err
		}

		refs = append(refs, Reference{dumpID, filter})
	}

	return refs, nil
}

func (db *DB) CountSameRepoPackageRefs(scheme, name, version string, visibleIDs []int) (int, error) {
	var qs []*sqlf.Query
	for _, id := range visibleIDs {
		qs = append(qs, sqlf.Sprintf("%d", id))
	}

	cq := sqlf.Sprintf(`
	SELECT COUNT(1) FROM lsif_references r
	WHERE r.scheme = %s AND r.name = %s AND r.version = %s AND r.dumpID = IN(%s)
`, scheme, name, version, sqlf.Join(qs, ", "))

	var totalCount int
	if err := db.db.QueryRowContext(context.Background(), cq.Query(sqlf.PostgresBindVar), cq.Args()...).Scan(&totalCount); err != nil {
		return 0, err
	}

	return totalCount, nil
}

func (db *DB) GetSameRepoPackageRefs(scheme, name, version string, visibleIDs []int, offset, limit int) ([]Reference, error) {
	var qs []*sqlf.Query
	for _, id := range visibleIDs {
		qs = append(qs, sqlf.Sprintf("%d", id))
	}

	queryx := sqlf.Sprintf(`
			SELECT d.id, r.filter FROM lsif_references r
			LEFT JOIN lsif_dumps d on r.dump_id = d.id
			WHERE r.scheme = $1 AND r.name = $2 AND r.version = $3 AND r.dump_id = ANY($4)
			ORDER BY d.root OFFSET $5 LIMIT $6
		`, scheme, name, version, sqlf.Join(qs, ", "), offset, limit)

	rows, err := db.db.QueryContext(context.Background(), queryx.Query(sqlf.PostgresBindVar), queryx.Args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []Reference
	for rows.Next() {
		var dumpID int
		var filter string

		if err := rows.Scan(&dumpID, &filter); err != nil {
			return nil, err
		}

		refs = append(refs, Reference{dumpID, filter})
	}

	return refs, nil
}

func (db *DB) GetPackage(scheme, name, version string) (Dump, bool, error) {
	query := `
		SELECT
			u.id,
			u.commit,
			u.root,
			u.visible_at_tip,
			u.uploaded_at,
			u.state,
			u.failure_summary,
			u.failure_stacktrace,
			u.started_at,
			u.finished_at,
			u.tracing_context,
			u.repository_id,
			u.indexer
		FROM lsif_packages p
		JOIN lsif_uploads u ON p.dump_id = u.id
		WHERE p.scheme = $1 AND p.name = $2 AND p.version = $3
		LIMIT 1
	`

	dump := Dump{}
	if err := db.db.QueryRowContext(context.Background(), query, scheme, name, version).Scan(
		&dump.ID,
		&dump.Commit,
		&dump.Root,
		&dump.VisibleAtTip,
		&dump.UploadedAt,
		&dump.State,
		&dump.FailureSummary,
		&dump.FailureStacktrace,
		&dump.StartedAt,
		&dump.FinishedAt,
		&dump.TracingContext,
		&dump.RepositoryID,
		&dump.Indexer,
	); err != nil {
		if err == sql.ErrNoRows {
			return Dump{}, false, nil
		}

		return Dump{}, false, err
	}

	return dump, false, nil
}
