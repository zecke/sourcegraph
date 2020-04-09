package server

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/keegancsmith/sqlf"
	"github.com/klauspost/compress/gzip"
	"github.com/sourcegraph/sourcegraph/cmd/frontend/db"
	"github.com/sourcegraph/sourcegraph/internal/api"
	"github.com/sourcegraph/sourcegraph/internal/db/dbutil"
	"github.com/sourcegraph/sourcegraph/internal/gitserver"
	"github.com/sourcegraph/sourcegraph/internal/trace/ot"
	"github.com/tomnomnom/linkheader"
)

const DefaultUploadPageSize = 50

type Server struct {
	host             string
	port             int
	bundleManagerURL string
	db               *sql.DB
}

type ServerOpts struct {
	Host             string
	Port             int
	BundleManagerURL string
	DB               *sql.DB
}

func New(opts ServerOpts) *Server {
	return &Server{
		host:             opts.Host,
		port:             opts.Port,
		bundleManagerURL: opts.BundleManagerURL,
		db:               opts.DB,
	}
}

func (s *Server) Start() error {
	addr := net.JoinHostPort(s.host, strconv.FormatInt(int64(s.port), 10))
	handler := ot.Middleware(s.handler())
	server := &http.Server{Addr: addr, Handler: handler}

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}

	return nil
}

// NOTE: the stuff below is pretty rough and I'm not planning on putting too much
// effort into this while we're doing the port. This is an internal API so it's
// allowed to be a bit shoddy during this transitionary period. I'm not even sure
// if HTTP is the right transport for the long term.

func (s *Server) handler() http.Handler {
	mux := mux.NewRouter()
	mux.Path("/uploads/{id:[0-9]+}").Methods("GET").HandlerFunc(s.handleGetUploadByID)
	mux.Path("/uploads/{id:[0-9]+}").Methods("DELETE").HandlerFunc(s.handleDeleteUploadByID)
	mux.Path("/uploads/repository/{id:[0-9]+}").Methods("GET").HandlerFunc(s.handleGetUploadsByRepo)
	mux.Path("/upload").Methods("POST").HandlerFunc(s.handleEnqueue)
	mux.Path("/exists").Methods("GET").HandlerFunc(s.handleExists)
	mux.Path("/definitions").Methods("GET").HandlerFunc(s.handleDefinitions)
	mux.Path("/references").Methods("GET").HandlerFunc(s.handleReferences)
	mux.Path("/hover").Methods("GET").HandlerFunc(s.handleHover)
	mux.Path("/uploads").Methods("POST").HandlerFunc(s.handleUploads)
	mux.Path("/prune").Methods("POST").HandlerFunc(s.handlePrune)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

// GET /uploads/{id:[0-9]+}
func (s *Server) handleGetUploadByID(w http.ResponseWriter, r *http.Request) {
	id := idFromRequest(r)

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

	upload := struct {
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
	}{}

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
			http.Error(w, "", http.StatusNotFound)
			return
		}

		fmt.Printf("WHOOPS: %s\n", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(upload)
}

// DELETE /uploads/{id:[0-9]+}
func (s *Server) handleDeleteUploadByID(w http.ResponseWriter, r *http.Request) {
	err := dbutil.Transaction(context.Background(), s.db, func(tx *sql.Tx) error {
		id := idFromRequest(r)
		query := "DELETE FROM lsif_uploads WHERE id = $1 RETURNING repository_id, visible_at_tip"

		var repositoryID int
		var visibleAtTip bool
		if err := tx.QueryRowContext(context.Background(), query, id).Scan(&repositoryID, &visibleAtTip); err != nil {
			return err
		}

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
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "", http.StatusNotFound)
			return
		}

		fmt.Printf("WHOOPS: %s\n", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GET /uploads/repository/{id:[0-9]+}
func (s *Server) handleGetUploadsByRepo(w http.ResponseWriter, r *http.Request) {
	id := idFromRequest(r)
	q := r.URL.Query()
	term := q.Get("query")
	state := q.Get("state")
	visibleAtTip, _ := strconv.ParseBool(q.Get("visibleAtTip"))
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil {
		limit = DefaultUploadPageSize
	}
	offset, _ := strconv.Atoi(q.Get("offset"))

	conds := []*sqlf.Query{
		sqlf.Sprintf("u.repository_id = %s", id),
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
		fmt.Printf("WHOOPS: %s\n", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var uploads []interface{}
	for rows.Next() {
		upload := struct {
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
		}{}

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

			fmt.Printf("WHOOPS: %s\n", err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}

		uploads = append(uploads, upload)
	}

	count := len(uploads) // TODO - perform this

	if offset+len(uploads) < count {
		q := r.URL.Query()
		q.Set("limit", strconv.FormatInt(int64(limit), 10))
		q.Set("offset", strconv.FormatInt(int64(offset+len(uploads)), 10))
		r.URL.RawQuery = q.Encode()
		link := linkheader.Link{
			URL: r.URL.String(),
			Rel: "next",
		}
		w.Header().Set("Link", link.String())
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"uploads": uploads, "totalCount": count})
}

// POST /upload
func (s *Server) handleEnqueue(w http.ResponseWriter, r *http.Request) {
	sanitize := func(s string) string {
		if s == "" || s == "/" {
			return ""
		}
		if strings.HasSuffix(s, "/") {
			return s
		}
		return s + "/"
	}

	q := r.URL.Query()
	repositoryID := q.Get("repositoryId")
	commit := q.Get("commit")
	root := sanitize(q.Get("root"))
	indexerName := q.Get("indexerName")

	f, err := ioutil.TempFile("", "upload-")
	if err != nil {
		fmt.Printf("WHOOPS: %s\n", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer os.Remove(f.Name())
	defer f.Close()

	if _, err := io.Copy(f, r.Body); err != nil {
		fmt.Printf("WHOOPS: %s\n", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	if indexerName == "" {
		if _, err := f.Seek(0, 0); err != nil {
			fmt.Printf("WHOOPS: %s\n", err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}

		gzipReader, err := gzip.NewReader(f)
		if err != nil {
			fmt.Printf("WHOOPS: %s\n", err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}

		line, isPrefix, err := bufio.NewReader(gzipReader).ReadLine()
		if err != nil {
			fmt.Printf("WHOOPS: %s\n", err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		if isPrefix {
			// OOF strange condition in these parts
			fmt.Printf("WHOOPS: %s\n", err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}

		meta := struct {
			Label    string `json:"label"`
			ToolInfo struct {
				Name string `json:"name"`
			} `json:"toolInfo"`
		}{}

		if err := json.Unmarshal(line, &meta); err != nil {
			fmt.Printf("WHOOPS R: %s\n", err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}

		if meta.Label != "metaData" || meta.ToolInfo.Name == "" {
			panic("no indexer name") // TODO
		}
		indexerName = meta.ToolInfo.Name
	}

	tracingContext := "" // TODO

	var id int
	if err := dbutil.Transaction(context.Background(), s.db, func(tx *sql.Tx) error {
		if err := s.db.QueryRowContext(
			context.Background(),
			`INSERT INTO lsif_uploads (commit, root, tracing_context, repository_id, indexer) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			commit, root, tracingContext, repositoryID, indexerName,
		).Scan(&id); err != nil {
			return err
		}

		if _, err := f.Seek(0, 0); err != nil {
			return err
		}

		url, err := url.Parse(fmt.Sprintf("%s/uploads/%d", s.bundleManagerURL, id))
		if err != nil {
			return err
		}

		req, err := http.NewRequest("POST", url.String(), f)
		if err != nil {
			return err
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("bad stauts %d", resp.StatusCode)
		}

		return nil
	}); err != nil {
		fmt.Printf("WHOOPS: %s\n", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(fmt.Sprintf(`{"id": %d}`, id)))
}

// GET /exists
func (s *Server) handleExists(w http.ResponseWriter, r *http.Request) {
	panic("unimplemented") // TODO
}

// GET /definitions
func (s *Server) handleDefinitions(w http.ResponseWriter, r *http.Request) {
	panic("unimplemented") // TODO
}

// GET /references
func (s *Server) handleReferences(w http.ResponseWriter, r *http.Request) {
	panic("unimplemented") // TODO
}

// GET /hover
func (s *Server) handleHover(w http.ResponseWriter, r *http.Request) {
	panic("unimplemented") // TODO
}

// POST /uploads
func (s *Server) handleUploads(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() // TODO - search for this, can remove everywhere safely

	payload := struct {
		IDs []int `json:"ids"`
	}{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		fmt.Printf("WHOOPS XXX: %s\n", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	var qs []*sqlf.Query
	for _, id := range payload.IDs {
		qs = append(qs, sqlf.Sprintf("%d", id))
	}

	query := sqlf.Sprintf("SELECT id, state FROM lsif_uploads WHERE id IN (%s)", sqlf.Join(qs, ", "))

	rows, err := s.db.QueryContext(context.Background(), query.Query(sqlf.PostgresBindVar), query.Args()...)
	if err != nil {
		fmt.Printf("WHOOPS: %s\n", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	states := map[int]string{}
	for rows.Next() {
		var id int
		var state string
		if err := rows.Scan(&id, &state); err != nil {
			fmt.Printf("WHOOPS: %s\n", err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}

		states[id] = state
	}

	_ = json.NewEncoder(w).Encode(states)
}

// POST /prune
func (s *Server) handlePrune(w http.ResponseWriter, r *http.Request) {
	query := "DELETE FROM lsif_uploads WHERE visible_at_tip = false ORDER BY uploaded_at LIMIT 1 RETURNING id"

	var id int
	if err := s.db.QueryRowContext(context.Background(), query).Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			_ = json.NewEncoder(w).Encode(nil)
			return
		}

		fmt.Printf("WHOOPS: %s\n", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id})
}

// idFromRequest returns the database id from the request URL's path. This method
// must only be called from routes containing the `id:[0-9]+` pattern, as the error
// return from ParseInt is not checked.
func idFromRequest(r *http.Request) int64 {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	return id
}

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
