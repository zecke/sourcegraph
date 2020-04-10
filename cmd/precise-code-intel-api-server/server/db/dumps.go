package db

import (
	"context"
	"database/sql"
	"time"

	"github.com/keegancsmith/sqlf"
)

type Dump struct {
	ID                int        `json:"id"`
	Commit            string     `json:"commit"`
	Root              string     `json:"root"`
	VisibleAtTip      bool       `json:"visibleAtTip"`
	UploadedAt        time.Time  `json:"uploadedAt"`
	State             string     `json:"state"`
	FailureSummary    *string    `json:"failureSummary"`
	FailureStacktrace *string    `json:"failureStacktrace"`
	StartedAt         *time.Time `json:"startedAt"`
	FinishedAt        *time.Time `json:"finishedAt"`
	TracingContext    string     `json:"tracingContext"`
	RepositoryID      int        `json:"repositoryId"`
	Indexer           string     `json:"indexer"`
	// TODO
	// ProcessedAt       time.Time  `json:"processedAt"`
}

func (db *DB) GetDumpByID(id int) (Dump, bool, error) {
	query := sqlf.Sprintf(`
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
		FROM lsif_uploads u WHERE id = %d
	`, id)

	var dump Dump
	if err := db.db.QueryRowContext(context.Background(), query.Query(sqlf.PostgresBindVar), query.Args()...).Scan(
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

	return dump, true, nil
}

func (db *DB) DoPrune() (int, bool, error) {
	// TODO - should only be completed
	query := "DELETE FROM lsif_uploads WHERE visible_at_tip = false ORDER BY uploaded_at LIMIT 1 RETURNING id"

	var id int
	if err := db.db.QueryRowContext(context.Background(), query).Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return 0, false, nil
		}

		return 0, false, err
	}

	return id, true, nil
}

func (db *DB) GetDumps(ids []int) (map[int]Dump, error) {
	var qs []*sqlf.Query
	for _, id := range ids {
		qs = append(qs, sqlf.Sprintf("%d", id))
	}

	query2 := sqlf.Sprintf(`SELECT
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
	FROM lsif_uploads u WHERE id IN (%s)`, sqlf.Join(qs, ", "))

	rows2, err := db.db.QueryContext(context.Background(), query2.Query(sqlf.PostgresBindVar), query2.Args()...)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()

	dumpsByID := map[int]Dump{}
	for rows2.Next() {
		dump := Dump{}
		if err := rows2.Scan(
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
			return nil, err
		}

		dumpsByID[dump.ID] = dump
	}

	return dumpsByID, nil
}

func (db *DB) FindClosestDumps(repositoryID int, commit, file string) ([]Dump, error) {
	query := "WITH " + bidirectionalLineage + ", " + visibleDumps + `
		SELECT d.dump_id FROM lineage_with_dumps d
		WHERE $3 LIKE (d.root || '%') AND d.dump_id IN (SELECT * FROM visible_ids)
		ORDER BY d.n
	`
	rows, err := db.db.QueryContext(context.Background(), query, repositoryID, commit, file)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var qs []*sqlf.Query
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		qs = append(qs, sqlf.Sprintf("%d", id))
	}

	query2 := sqlf.Sprintf(`SELECT
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
	FROM lsif_uploads u WHERE id IN (%s)`, sqlf.Join(qs, ", "))

	rows2, err := db.db.QueryContext(context.Background(), query2.Query(sqlf.PostgresBindVar), query2.Args()...)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()

	var dumps []Dump
	for rows2.Next() {
		dump := Dump{}
		if err := rows2.Scan(
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
			return nil, err
		}

		// TODO - need to de-duplicate
		dumps = append(dumps, dump)
	}

	return dumps, nil
}
