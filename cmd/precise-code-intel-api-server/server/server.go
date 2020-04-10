package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/inconshreveable/log15"
	"github.com/sourcegraph/sourcegraph/cmd/precise-code-intel-api-server/server/db"
	"github.com/sourcegraph/sourcegraph/internal/trace/ot"
	"github.com/tomnomnom/linkheader"
)

const DefaultUploadPageSize = 50

type Server struct {
	host             string
	port             int
	bundleManagerURL string
	db               *db.DB
}

type ServerOpts struct {
	Host             string
	Port             int
	BundleManagerURL string
	DB               *db.DB
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
		// TODO - should fatal instead
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
	upload, exists, err := s.db.GetUploadByID(int(idFromRequest(r)))
	if err != nil {
		log15.Error("Failed to retrieve upload", "error", err)
		http.Error(w, fmt.Sprintf("failed to retrieve upload: %s", err.Error()), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "upload not found", http.StatusNotFound)
		return
	}

	writeJSON(w, upload)
}

// DELETE /uploads/{id:[0-9]+}
func (s *Server) handleDeleteUploadByID(w http.ResponseWriter, r *http.Request) {
	exists, err := s.db.DeleteUploadByID(int(idFromRequest(r)))
	if err != nil {
		log15.Error("Failed to delete upload", "error", err)
		http.Error(w, fmt.Sprintf("failed to delete upload: %s", err.Error()), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "upload not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GET /uploads/repository/{id:[0-9]+}
func (s *Server) handleGetUploadsByRepo(w http.ResponseWriter, r *http.Request) {
	id := int(idFromRequest(r))
	q := r.URL.Query()
	term := q.Get("query")
	state := q.Get("state")
	visibleAtTip, _ := strconv.ParseBool(q.Get("visibleAtTip"))
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil {
		limit = DefaultUploadPageSize
	}
	offset, _ := strconv.Atoi(q.Get("offset"))

	uploads, totalCount, err := s.db.GetUploadsByRepo(id, state, term, visibleAtTip, limit, offset)
	if err != nil {
		log15.Error("Failed to list uploads", "error", err)
		http.Error(w, fmt.Sprintf("failed to list uploads: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	// TODO - update this
	if offset+len(uploads) < totalCount {
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

	writeJSON(w, map[string]interface{}{"uploads": uploads, "totalCount": totalCount})
}

// POST /upload
func (s *Server) handleEnqueue(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	repositoryID, _ := strconv.Atoi(q.Get("repositoryId"))
	commit := q.Get("commit")
	root := sanitizeRoot(q.Get("root"))
	indexerName := q.Get("indexerName")

	f, err := ioutil.TempFile("", "upload-")
	if err != nil {
		log15.Error("Failed to open target file", "error", err)
		http.Error(w, fmt.Sprintf("failed to open target file: %s", err.Error()), http.StatusInternalServerError)
		return
	}
	defer os.Remove(f.Name())
	defer f.Close()

	if _, err := io.Copy(f, r.Body); err != nil {
		log15.Error("Failed to write payload", "error", err)
		http.Error(w, fmt.Sprintf("failed to write payload: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	// TODO - implement
	tracingContext := "{}"

	if indexerName == "" {
		if indexerName, err = readIndexerNameFromFile(f); err != nil {
			log15.Error("Failed to read indexer name from upload", "error", err)
			http.Error(w, fmt.Sprintf("failed to read indexer name from upload: %s", err.Error()), http.StatusInternalServerError)
			return
		}
	}

	id, err := s.db.Enqueue(commit, root, tracingContext, repositoryID, indexerName, func(id int) error {
		return sendUpload(s.bundleManagerURL, id, f)
	})
	if err != nil {
		log15.Error("Failed to enqueue payload", "error", err)
		http.Error(w, fmt.Sprintf("failed to enqueue payload: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]interface{}{"id": id})
}

// GET /exists
func (s *Server) handleExists(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	repositoryID, _ := strconv.Atoi(q.Get("repositoryId"))
	commit := q.Get("commit")
	file := q.Get("path")

	dumps, err := s.findClosestDatabase(repositoryID, commit, file)
	if err != nil {
		log15.Error("Failed to handle exists request", "error", err)
		http.Error(w, fmt.Sprintf("failed to handle exists request: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{"uploads": dumps})
}

// GET /definitions
func (s *Server) handleDefinitions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	file := q.Get("path")
	line, _ := strconv.Atoi(q.Get("line"))
	character, _ := strconv.Atoi(q.Get("character"))
	uploadID, _ := strconv.Atoi(q.Get("uploadId"))

	defs, err := s.getDefs(file, line, character, uploadID)
	if err != nil {
		if err == ErrMissingDump {
			http.Error(w, "no such dump", http.StatusNotFound)
			return
		}

		log15.Error("Failed to handle definitions request", "error", err)
		http.Error(w, fmt.Sprintf("failed to handle definitions request: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	outers, err := s.serializeLocations(defs)
	if err != nil {
		log15.Error("Failed to resolve locations", "error", err)
		http.Error(w, fmt.Sprintf("failed to resolve locations: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{"locations": outers})
}

// GET /references
func (s *Server) handleReferences(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	repositoryID, _ := strconv.Atoi(q.Get("repositoryId"))
	commit := q.Get("commit")
	limit, _ := strconv.Atoi(q.Get("limit"))

	cursor, err := s.decodeCursor(r)
	if err != nil {
		if err == ErrMissingDump {
			http.Error(w, "no such dump", http.StatusNotFound)
			return
		}

		log15.Error("Failed to prepare cursor", "error", err)
		http.Error(w, fmt.Sprintf("failed to prepare cursor: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	p, err := s.getRefs(repositoryID, commit, limit, cursor)
	if err != nil {
		log15.Error("Failed to handle references request", "error", err)
		http.Error(w, fmt.Sprintf("failed to handle references request: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	outers, err := s.serializeLocations(p.locations)
	if err != nil {
		log15.Error("Failed to resolve locations", "error", err)
		http.Error(w, fmt.Sprintf("failed to resolve locations: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	if p.newCursor != nil {
		// TODO - implement
	}

	writeJSON(w, map[string]interface{}{"locations": outers})
}

// GET /hover
func (s *Server) handleHover(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	file := q.Get("path")
	line, _ := strconv.Atoi(q.Get("line"))
	character, _ := strconv.Atoi(q.Get("character"))
	uploadID, _ := strconv.Atoi(q.Get("uploadId"))

	text, rn, ok, err := s.getHover(file, line, character, uploadID)
	if err != nil {
		if err == ErrMissingDump {
			http.Error(w, "no such dump", http.StatusNotFound)
			return
		}

		log15.Error("Failed to handle hover request", "error", err)
		http.Error(w, fmt.Sprintf("failed to handle hover request: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	if !ok {
		writeJSON(w, nil)
	} else {
		writeJSON(w, map[string]interface{}{"text": text, "range": rn})
	}
}

// POST /uploads
func (s *Server) handleUploads(w http.ResponseWriter, r *http.Request) {
	payload := struct {
		IDs []int `json:"ids"`
	}{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log15.Error("Failed to read request body", "error", err)
		http.Error(w, fmt.Sprintf("failed to read request body: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	states, err := s.db.GetStates(payload.IDs)
	if err != nil {
		log15.Error("Failed to retrieve upload states", "error", err)
		http.Error(w, fmt.Sprintf("failed to retrieve upload states: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	pairs := []interface{}{}
	for k, v := range states {
		pairs = append(pairs, []interface{}{k, v})
	}

	writeJSON(w, map[string]interface{}{"type": "map", "value": pairs})
}

// POST /prune
func (s *Server) handlePrune(w http.ResponseWriter, r *http.Request) {
	id, prunable, err := s.db.DoPrune()
	if err != nil {
		log15.Error("Failed to prune upload", "error", err)
		http.Error(w, fmt.Sprintf("failed to prune upload: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	if !prunable {
		writeJSON(w, nil)
	} else {
		writeJSON(w, map[string]interface{}{"id": id})
	}
}

// idFromRequest returns the database id from the request URL's path. This method
// must only be called from routes containing the `id:[0-9]+` pattern, as the error
// return from ParseInt is not checked.
func idFromRequest(r *http.Request) int64 {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	return id
}

// copyAll writes the contents of r to w and logs on write failure.
func copyAll(w http.ResponseWriter, r io.Reader) {
	if _, err := io.Copy(w, r); err != nil {
		log15.Error("Failed to write payload to client", "error", err)
	}
}

// writeJSON writes the JSON-encoded payload to w and logs on write failure.
// If there is an encoding error, then a 500-level status is written to w.
func writeJSON(w http.ResponseWriter, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		log15.Error("Failed to serialize result", "error", err)
		http.Error(w, fmt.Sprintf("failed to serialize result: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	copyAll(w, bytes.NewReader(data))
}

func sanitizeRoot(s string) string {
	if s == "" || s == "/" {
		return ""
	}
	if strings.HasSuffix(s, "/") {
		return s
	}
	return s + "/"
}
