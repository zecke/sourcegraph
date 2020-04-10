package db

import (
	"bytes"
	"context"
	"database/sql"
	"time"

	"github.com/keegancsmith/sqlf"
	sgdb "github.com/sourcegraph/sourcegraph/cmd/frontend/db"
	"github.com/sourcegraph/sourcegraph/internal/api"
	"github.com/sourcegraph/sourcegraph/internal/db/dbutil"
	"github.com/sourcegraph/sourcegraph/internal/gitserver"
)

const StalledUploadMaxAge = time.Second * 5

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

func (db *DB) GetUploadByID(id int) (Upload, bool, error) {
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

	row := db.db.QueryRowContext(context.Background(), query, id)

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

func (db *DB) GetUploadsByRepo(repositoryID int, state, term string, visibleAtTip bool, limit, offset int) ([]Upload, int, error) {
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

	rows, err := db.db.QueryContext(context.Background(), query.Query(sqlf.PostgresBindVar), query.Args()...)
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
func (db *DB) Enqueue(commit, root, tracingContext string, repositoryID int, indexerName string, callback func(id int) error) (int, error) {
	var id int
	err := dbutil.Transaction(context.Background(), db.db, func(tx *sql.Tx) error {
		if err := db.db.QueryRowContext(
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

func (db *DB) GetStates(ids []int) (map[int]string, error) {
	var qs []*sqlf.Query
	for _, id := range ids {
		qs = append(qs, sqlf.Sprintf("%d", id))
	}

	query := sqlf.Sprintf("SELECT id, state FROM lsif_uploads WHERE id IN (%s)", sqlf.Join(qs, ", "))

	rows, err := db.db.QueryContext(context.Background(), query.Query(sqlf.PostgresBindVar), query.Args()...)
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

func (db *DB) DeleteUploadByID(id int) (found bool, err error) {
	err = dbutil.Transaction(context.Background(), db.db, func(tx *sql.Tx) error {
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
			repo, err := sgdb.Repos.Get(context.Background(), api.RepoID(repositoryID))
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

func (db *DB) ResetStalled() ([]int, error) {
	query := `
		UPDATE lsif_uploads u SET state = 'queued', started_at = null WHERE id = ANY(
			SELECT id FROM lsif_uploads
			WHERE state = 'processing' AND started_at < now() - ($1 * interval '1 second')
			FOR UPDATE SKIP LOCKED
		)
		RETURNING u.id
	`

	rows, err := db.db.QueryContext(context.Background(), query, StalledUploadMaxAge/time.Second)
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
