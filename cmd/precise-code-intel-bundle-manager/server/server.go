package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gorilla/mux"
)

type Server struct {
	storageDir           string
	databaseCache        *DatabaseCache
	documentDataCache    *DocumentDataCache
	resultChunkDataCache *ResultChunkDataCache
}

const DatabaseCacheSize = 100
const DocumentDataCacheSize = 100
const ResultChunkDataCacheSize = 100

func New(storageDir string) (*Server, error) {
	databaseCache, err := NewDatabaseCache(DatabaseCacheSize)
	if err != nil {
		return nil, err
	}

	documentDataCache, err := NewDocumentDataCache(DocumentDataCacheSize)
	if err != nil {
		return nil, err
	}

	resultChunkDataCache, err := NewResultChunkDataCache(ResultChunkDataCacheSize)
	if err != nil {
		return nil, err
	}

	return &Server{
		storageDir:           storageDir,
		databaseCache:        databaseCache,
		documentDataCache:    documentDataCache,
		resultChunkDataCache: resultChunkDataCache,
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := mux.NewRouter()
	mux.Path("/uploads/{id:[0-9]+}").Methods("GET").HandlerFunc(s.handleGetUpload)
	mux.Path("/uploads/{id:[0-9]+}").Methods("POST").HandlerFunc(s.handlePostUpload)
	mux.Path("/dbs/{id:[0-9]+}").Methods("POST").HandlerFunc(s.handlePostDatabase)
	mux.Path("/dbs/{id:[0-9]+}/exists").Methods("GET").HandlerFunc(s.handleExists)
	mux.Path("/dbs/{id:[0-9]+}/definitions").Methods("GET").HandlerFunc(s.handleDefinitions)
	mux.Path("/dbs/{id:[0-9]+}/references").Methods("GET").HandlerFunc(s.handleReferences)
	mux.Path("/dbs/{id:[0-9]+}/hover").Methods("GET").HandlerFunc(s.handleHover)
	mux.Path("/dbs/{id:[0-9]+}/monikersByPosition").Methods("GET").HandlerFunc(s.handleMonikerByPosition)
	mux.Path("/dbs/{id:[0-9]+}/monikerResults").Methods("GET").HandlerFunc(s.handleMonikerResults)
	mux.Path("/dbs/{id:[0-9]+}/packageInformation").Methods("GET").HandlerFunc(s.handlePackageInformation)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

func (s *Server) handleGetUpload(w http.ResponseWriter, r *http.Request) {
	file, err := os.Open(s.uploadFilename(idFromRequest(r)))
	if err != nil {
		http.Error(w, "Upload not found.", http.StatusNotFound)
		return
	}
	defer file.Close()

	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, file)
}

func (s *Server) handlePostUpload(w http.ResponseWriter, r *http.Request) {
	s.doUpload(w, r, s.uploadFilename)
}

func (s *Server) handlePostDatabase(w http.ResponseWriter, r *http.Request) {
	s.doUpload(w, r, s.dbFilename)
}

func (s *Server) doUpload(w http.ResponseWriter, r *http.Request, makeFilename func(id int64) string) {
	defer r.Body.Close()

	targetFile, err := os.OpenFile(makeFilename(idFromRequest(r)), os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(targetFile, r.Body)
}

func (s *Server) handleExists(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path := q.Get("path")

	if err := s.withDatabase(r, func(db *Database) error {
		exists, err := db.Exists(path)
		if err != nil {
			return err
		}

		return json.NewEncoder(w).Encode(exists)
	}); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleDefinitions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path := q.Get("path")
	line, _ := strconv.Atoi(q.Get("line"))
	character, _ := strconv.Atoi(q.Get("character"))

	if err := s.withDatabase(r, func(db *Database) error {
		locations, err := db.Definitions(path, line, character)
		if err != nil {
			return err
		}

		return json.NewEncoder(w).Encode(locations)
	}); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleReferences(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path := q.Get("path")
	line, _ := strconv.Atoi(q.Get("line"))
	character, _ := strconv.Atoi(q.Get("character"))

	if err := s.withDatabase(r, func(db *Database) error {
		locations, err := db.References(path, line, character)
		if err != nil {
			return err
		}

		return json.NewEncoder(w).Encode(locations)
	}); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleHover(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path := q.Get("path")
	line, _ := strconv.Atoi(q.Get("line"))
	character, _ := strconv.Atoi(q.Get("character"))

	if err := s.withDatabase(r, func(db *Database) error {
		text, hoverRange, exists, err := db.Hover(path, line, character)
		if err != nil {
			return err
		}

		var resp interface{}
		if exists {
			resp = struct {
				Text  string `json:"text"`
				Range Range  `json:"range"`
			}{
				Text:  text,
				Range: hoverRange,
			}
		} else {
			resp = nil
		}

		return json.NewEncoder(w).Encode(resp)
	}); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleMonikerByPosition(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path := q.Get("path")
	line, _ := strconv.Atoi(q.Get("line"))
	character, _ := strconv.Atoi(q.Get("character"))

	if err := s.withDatabase(r, func(db *Database) error {
		monikerData, err := db.MonikerByPosition(path, line, character)
		if err != nil {
			return err
		}

		return json.NewEncoder(w).Encode(monikerData)
	}); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleMonikerResults(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	modelType := q.Get("modelType")
	scheme := q.Get("scheme")
	identifier := q.Get("identifier")
	skip, _ := strconv.Atoi(q.Get("skip"))
	take, err := strconv.Atoi(q.Get("take"))
	if err != nil {
		take = 100
	}

	var tableName string
	if modelType == "definition" {
		tableName = "definitions"
	} else if modelType == "reference" {
		tableName = "references"
	} else {
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	if err := s.withDatabase(r, func(db *Database) error {
		locations, count, err := db.MonikerResults(tableName, scheme, identifier, skip, take)
		if err != nil {
			return err
		}

		resp := struct {
			Locations []InternalLocation `json:"locations"`
			Count     int                `json:"count"`
		}{
			Locations: locations,
			Count:     count,
		}

		return json.NewEncoder(w).Encode(resp)
	}); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handlePackageInformation(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path := q.Get("path")
	packageInformationID := ID(q.Get("packageInformationId"))

	if err := s.withDatabase(r, func(db *Database) error {
		packageInformationData, exists, err := db.PackageInformation(path, packageInformationID)
		if err != nil {
			return err
		}

		var resp interface{}
		if exists {
			resp = packageInformationData
		} else {
			resp = nil
		}

		return json.NewEncoder(w).Encode(resp)
	}); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
}

func (s *Server) withDatabase(r *http.Request, handler func(db *Database) error) error {
	filename := s.dbFilename(idFromRequest(r))
	openDatabase := func() (*Database, error) { return OpenDatabase(filename, s.documentDataCache, s.resultChunkDataCache) }

	return s.databaseCache.WithDatabase(filename, openDatabase, handler)
}

func (s *Server) uploadFilename(id int64) string {
	return filepath.Join(s.storageDir, "uploads", fmt.Sprintf("%d.lsif.gz", id))
}

func (s *Server) dbFilename(id int64) string {
	return filepath.Join(s.storageDir, "dbs", fmt.Sprintf("%d.lsif.db", id))
}

func idFromRequest(r *http.Request) int64 {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	return id
}
