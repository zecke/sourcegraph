package server

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/keegancsmith/sqlf"
	"github.com/sourcegraph/sourcegraph/cmd/frontend/db"
	"github.com/sourcegraph/sourcegraph/internal/api"
	"github.com/sourcegraph/sourcegraph/internal/db/dbutil"
	"github.com/sourcegraph/sourcegraph/internal/gitserver"
)

var ancestorLineage = `
	RECURSIVE lineage(id, "commit", parent, repository_id) AS (
		SELECT c.* FROM lsif_commits c WHERE c.repository_id = $1 AND c."commit" = $2
		UNION
		SELECT c.* FROM lineage a JOIN lsif_commits c ON a.repository_id = c.repository_id AND a.parent = c."commit"
	)
`

var bidirectionalLineage = `
	RECURSIVE lineage(id, "commit", parent_commit, repository_id, direction) AS (
		SELECT l.* FROM (
			-- seed recursive set with commit looking in ancestor direction
			SELECT c.*, 'A' FROM lsif_commits c WHERE c.repository_id = $1 AND c."commit" = $2
			UNION
			-- seed recursive set with commit looking in descendant direction
			SELECT c.*, 'D' FROM lsif_commits c WHERE c.repository_id = $1 AND c."commit" = $2
		) l

		UNION

		SELECT * FROM (
			WITH l_inner AS (SELECT * FROM lineage)
			-- get next ancestors (multiple parents for merge commits)
			SELECT c.*, 'A' FROM l_inner l JOIN lsif_commits c ON l.direction = 'A' AND c.repository_id = l.repository_id AND c."commit" = l.parent_commit
			UNION
			-- get next descendants
			SELECT c.*, 'D' FROM l_inner l JOIN lsif_commits c ON l.direction = 'D' and c.repository_id = l.repository_id AND c.parent_commit = l."commit"
		) subquery
	)
`

var visibleDumps = lineageWithDumps + `,
	visible_ids AS (
		-- Remove dumps where there exists another visible dump of smaller depth with an
		-- overlapping root from the same indexer. Such dumps would not be returned with
		-- a closest commit query so we don't want to return results for them in global
		-- find-reference queries either.
		SELECT DISTINCT t1.dump_id as id FROM lineage_with_dumps t1 WHERE NOT EXISTS (
			SELECT 1 FROM lineage_with_dumps t2
			WHERE t2.n < t1.n AND t1.indexer = t2.indexer AND (
				t2.root LIKE (t1.root || '%') OR
				t1.root LIKE (t2.root || '%')
			)
		)
	)
`

const MaxTraversalLimit = 100

var lineageWithDumps = fmt.Sprintf(`
	-- Limit the visibility to the maximum traversal depth and approximate
	-- each commit's depth by its row number.
	limited_lineage AS (
		SELECT a.*, row_number() OVER() as n from lineage a LIMIT %d
	),
	-- Correlate commits to dumps and filter out commits without LSIF data
	lineage_with_dumps AS (
		SELECT a.*, d.root, d.indexer, d.id as dump_id FROM limited_lineage a
		JOIN lsif_dumps d ON d.repository_id = a.repository_id AND d."commit" = a."commit"
	)
`, MaxTraversalLimit)

type Upload struct {
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
	Rank              *int       `json:"placeInQueue"`
	// TODO - add this as an optional field
	// ProcessedAt       time.Time  `json:"processedAt"`
}

func (s *Server) getUploadByID(id int) (Upload, bool, error) {
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
			u.indexer,
			s.rank
		FROM lsif_uploads u
		LEFT JOIN (
			SELECT r.id, RANK() OVER (ORDER BY r.uploaded_at) as rank
			FROM lsif_uploads r
			WHERE r.state = 'queued'
		) s
		ON u.id = s.id
		WHERE u.id = $1
	`

	row := s.db.QueryRowContext(context.Background(), query, id)

	upload := Upload{}
	if err := row.Scan(
		&upload.ID,
		&upload.Commit,
		&upload.Root,
		&upload.VisibleAtTip,
		&upload.UploadedAt,
		&upload.State,
		&upload.FailureSummary,
		&upload.FailureStacktrace,
		&upload.StartedAt,
		&upload.FinishedAt,
		&upload.TracingContext,
		&upload.RepositoryID,
		&upload.Indexer,
		&upload.Rank,
	); err != nil {
		if err == sql.ErrNoRows {
			return Upload{}, false, nil
		}

		return Upload{}, false, err
	}

	return upload, true, nil
}

func (s *Server) getUploadsByRepo(repositoryID int, state, term string, visibleAtTip bool, limit, offset int) ([]Upload, int, error) {
	conds := []*sqlf.Query{
		sqlf.Sprintf("u.repository_id = %s", repositoryID),
	}
	if state != "" {
		conds = append(conds, sqlf.Sprintf("state = %s", state))
	}
	if term != "" {
		var termConds []*sqlf.Query
		for _, column := range []string{"commit", "root", "indexer", "failure_summary", "failure_stacktrace"} {
			termConds = append(termConds, sqlf.Sprintf(column+" LIKE %s", "%"+term+"%"))
		}

		conds = append(conds, sqlf.Sprintf("(%s)", sqlf.Join(termConds, " OR ")))
	}
	if visibleAtTip {
		conds = append(conds, sqlf.Sprintf("visible_at_tip = true"))
	}

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
			u.indexer,
			s.rank
		FROM lsif_uploads u
		LEFT JOIN (
			SELECT r.id, RANK() OVER (ORDER BY r.uploaded_at) as rank
			FROM lsif_uploads r
			WHERE r.state = 'queued'
		) s
		ON u.id = s.id
		WHERE %s
		ORDER BY uploaded_at DESC
		LIMIT %d
		OFFSET %d
	`, sqlf.Join(conds, " AND "), limit, offset)

	rows, err := s.db.QueryContext(context.Background(), query.Query(sqlf.PostgresBindVar), query.Args()...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var uploads []Upload
	for rows.Next() {
		upload := Upload{}
		if err := rows.Scan(
			&upload.ID,
			&upload.Commit,
			&upload.Root,
			&upload.VisibleAtTip,
			&upload.UploadedAt,
			&upload.State,
			&upload.FailureSummary,
			&upload.FailureStacktrace,
			&upload.StartedAt,
			&upload.FinishedAt,
			&upload.TracingContext,
			&upload.RepositoryID,
			&upload.Indexer,
			&upload.Rank,
		); err != nil {
			return nil, 0, err
		}

		uploads = append(uploads, upload)
	}

	count := len(uploads) // TODO - implement
	return uploads, count, nil
}

// TODO - find a better pattern for this
func (s *Server) enqueue(commit, root, tracingContext string, repositoryID int, indexerName string, callback func(id int) error) (int, error) {
	var id int
	err := dbutil.Transaction(context.Background(), s.db, func(tx *sql.Tx) error {
		if err := s.db.QueryRowContext(
			context.Background(),
			`INSERT INTO lsif_uploads (commit, root, tracing_context, repository_id, indexer) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			commit, root, tracingContext, repositoryID, indexerName,
		).Scan(&id); err != nil {
			return err
		}

		return callback(id)
	})

	return id, err
}

func (s *Server) getStates(ids []int) (map[int]string, error) {
	var qs []*sqlf.Query
	for _, id := range ids {
		qs = append(qs, sqlf.Sprintf("%d", id))
	}

	query := sqlf.Sprintf("SELECT id, state FROM lsif_uploads WHERE id IN (%s)", sqlf.Join(qs, ", "))

	rows, err := s.db.QueryContext(context.Background(), query.Query(sqlf.PostgresBindVar), query.Args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	states := map[int]string{}
	for rows.Next() {
		var id int
		var state string
		if err := rows.Scan(&id, &state); err != nil {
			return nil, err
		}

		states[id] = state
	}

	return states, nil
}

func (s *Server) doPrune() (int, bool, error) {
	query := "DELETE FROM lsif_uploads WHERE visible_at_tip = false ORDER BY uploaded_at LIMIT 1 RETURNING id"

	var id int
	if err := s.db.QueryRowContext(context.Background(), query).Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return 0, false, nil
		}

		return 0, false, err
	}

	return id, true, nil
}

func (s *Server) deleteUploadByID(id int) (found bool, err error) {

	err = dbutil.Transaction(context.Background(), s.db, func(tx *sql.Tx) error {
		query := "DELETE FROM lsif_uploads WHERE id = $1 RETURNING repository_id, visible_at_tip"

		var repositoryID int
		var visibleAtTip bool
		if err := tx.QueryRowContext(context.Background(), query, id).Scan(&repositoryID, &visibleAtTip); err != nil {
			if err == sql.ErrNoRows {
				found = false
				return nil
			}

			return err
		}

		found = true
		if visibleAtTip {
			// TODO - do we need this dependency?
			repo, err := db.Repos.Get(context.Background(), api.RepoID(repositoryID))
			if err != nil {
				return err
			}

			cmd := gitserver.DefaultClient.Command("git", "rev-parse", "HEAD")
			cmd.Repo = gitserver.Repo{Name: repo.Name}
			out, err := cmd.CombinedOutput(context.Background())
			if err != nil {
				return err
			}
			tipCommit := string(bytes.TrimSpace(out))

			// TODO - do we need to discover commits here? The old
			// implementation does it but my gut says no now that
			// I think about it a bit more.

			query2 := "WITH " + ancestorLineage + ", " + visibleDumps + `
				-- Update dump records by:
				--   (1) unsetting the visibility flag of all previously visible dumps, and
				--   (2) setting the visibility flag of all currently visible dumps
				UPDATE lsif_dumps d
				SET visible_at_tip = id IN (SELECT * from visible_ids)
				WHERE d.repository_id = $1 AND (d.id IN (SELECT * from visible_ids) OR d.visible_at_tip)
			`

			if _, err := tx.ExecContext(context.Background(), query2, repositoryID, tipCommit); err != nil {
				return err
			}
		}

		return nil
	})
	return
}

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

func (s *Server) getDumpByID(id int) (Dump, bool, error) {
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
	if err := s.db.QueryRowContext(context.Background(), query.Query(sqlf.PostgresBindVar), query.Args()...).Scan(
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

type Reference struct {
	DumpID int
	Filter string
}

func (s *Server) getVisibleIDs(repositoryID int, commit string) ([]int, error) {
	rows, err := s.db.QueryContext(context.Background(), "WITH "+bidirectionalLineage+", "+visibleDumps+"SELECT id FROM visible_ids", repositoryID, commit)
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

func (s *Server) countPackageRefs(scheme, name, version string, repositoryID int) (int, error) {
	query := `
		SELECT COUNT(1) FROM lsif_references r
		LEFT JOIN lsif_dumps d ON r.dump_id = d.id
		WHERE scheme = $1 AND name = $2 AND version = $3 AND d.repository_id != $4 AND d.visible_at_tip = true
	`

	var totalCount int
	if err := s.db.QueryRowContext(context.Background(), query, scheme, name, version, repositoryID).Scan(&totalCount); err != nil {
		return 0, err
	}

	return totalCount, nil
}

func (s *Server) getPackageRefs(scheme, name, version string, repositoryID, limit, offset int) ([]Reference, error) {
	queryx := `
			SELECT d.id, r.filter FROM lsif_references r
			LEFT JOIN lsif_dumps d ON r.dump_id = d.id
			WHERE scheme = $1 AND name = $2 AND version = $3 AND d.repository_id != $4 AND d.visible_at_tip = true
			ORDER BY d.repository_id, d.root
			LIMIT $5
			OFFSET $6
		`

	rows, err := s.db.QueryContext(context.Background(), queryx, scheme, name, version, repositoryID, limit, offset)
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

func (s *Server) countSameRepoPackageRefs(scheme, name, version string, visibleIDs []int) (int, error) {
	var qs []*sqlf.Query
	for _, id := range visibleIDs {
		qs = append(qs, sqlf.Sprintf("%d", id))
	}

	cq := sqlf.Sprintf(`
	SELECT COUNT(1) FROM lsif_references r
	WHERE r.scheme = %s AND r.name = %s AND r.version = %s AND r.dumpID = IN(%s)
`, scheme, name, version, sqlf.Join(qs, ", "))

	var totalCount int
	if err := s.db.QueryRowContext(context.Background(), cq.Query(sqlf.PostgresBindVar), cq.Args()...).Scan(&totalCount); err != nil {
		return 0, err
	}

	return totalCount, nil
}

func (s *Server) getSameRepoPackageRefs(scheme, name, version string, visibleIDs []int, offset, limit int) ([]Reference, error) {
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

	rows, err := s.db.QueryContext(context.Background(), queryx.Query(sqlf.PostgresBindVar), queryx.Args()...)
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

func (s *Server) getDumps(ids []int) (map[int]Dump, error) {
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

	rows2, err := s.db.QueryContext(context.Background(), query2.Query(sqlf.PostgresBindVar), query2.Args()...)
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

func (s *Server) findClosestDumps(repositoryID int, commit, file string) ([]Dump, error) {
	query := "WITH " + bidirectionalLineage + ", " + visibleDumps + `
		SELECT d.dump_id FROM lineage_with_dumps d
		WHERE $3 LIKE (d.root || '%') AND d.dump_id IN (SELECT * FROM visible_ids)
		ORDER BY d.n
	`
	rows, err := s.db.QueryContext(context.Background(), query, repositoryID, commit, file)
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

	rows2, err := s.db.QueryContext(context.Background(), query2.Query(sqlf.PostgresBindVar), query2.Args()...)
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

func (s *Server) getPackage(scheme, name, version string) (Dump, bool, error) {
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
	if err := s.db.QueryRowContext(context.Background(), query, scheme, name, version).Scan(
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

func (j *Janitor) cleanOld() ([]int, error) {
	query := `
		UPDATE lsif_uploads u SET state = 'queued', started_at = null WHERE id = ANY(
			SELECT id FROM lsif_uploads
			WHERE state = 'processing' AND started_at < now() - ($1 * interval '1 second')
			FOR UPDATE SKIP LOCKED
		)
		RETURNING u.id
	`

	rows, err := j.db.QueryContext(context.Background(), query, StalledUploadMaxAge/time.Second)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		ids = append(ids, id)
	}

	return ids, nil
}
