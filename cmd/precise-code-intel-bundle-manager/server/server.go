package server

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/sourcegraph/sourcegraph/internal/trace/ot"
)

type Server struct {
	host                 string
	port                 int
	bundleDir            string
	databaseCache        *DatabaseCache
	documentDataCache    *DocumentDataCache
	resultChunkDataCache *ResultChunkDataCache
}

type ServerOpts struct {
	Host                     string
	Port                     int
	BundleDir                string
	DatabaseCacheSize        int
	DocumentDataCacheSize    int
	ResultChunkDataCacheSize int
}

func New(opts ServerOpts) (*Server, error) {
	databaseCache, err := NewDatabaseCache(opts.DatabaseCacheSize)
	if err != nil {
		return nil, err
	}

	documentDataCache, err := NewDocumentDataCache(opts.DocumentDataCacheSize)
	if err != nil {
		return nil, err
	}

	resultChunkDataCache, err := NewResultChunkDataCache(opts.ResultChunkDataCacheSize)
	if err != nil {
		return nil, err
	}

	return &Server{
		host:                 opts.Host,
		port:                 opts.Port,
		bundleDir:            opts.BundleDir,
		databaseCache:        databaseCache,
		documentDataCache:    documentDataCache,
		resultChunkDataCache: resultChunkDataCache,
	}, nil
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
	mux.Path("/uploads/{id:[0-9]+}").Methods("GET").HandlerFunc(s.handleGetUpload)
	mux.Path("/uploads/{id:[0-9]+}").Methods("POST").HandlerFunc(s.handlePostUpload)
	mux.Path("/dbs/{id:[0-9]+}").Methods("POST").HandlerFunc(s.handlePostDatabase)
	mux.Path("/dbs/{id:[0-9]+}/exists").Methods("GET").HandlerFunc(s.handleExists)
	mux.Path("/dbs/{id:[0-9]+}/definitions").Methods("GET").HandlerFunc(s.handleDefinitions)
	mux.Path("/dbs/{id:[0-9]+}/references").Methods("GET").HandlerFunc(s.handleReferences)
	mux.Path("/dbs/{id:[0-9]+}/hover").Methods("GET").HandlerFunc(s.handleHover)
	mux.Path("/dbs/{id:[0-9]+}/monikersByPosition").Methods("GET").HandlerFunc(s.handleMonikersByPosition)
	mux.Path("/dbs/{id:[0-9]+}/monikerResults").Methods("GET").HandlerFunc(s.handleMonikerResults)
	mux.Path("/dbs/{id:[0-9]+}/packageInformation").Methods("GET").HandlerFunc(s.handlePackageInformation)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

func (s *Server) handleGetUpload(w http.ResponseWriter, r *http.Request) {
	file, err := os.Open(uploadFilename(s.bundleDir, idFromRequest(r)))
	if err != nil {
		http.Error(w, "Upload not found.", http.StatusNotFound)
		return
	}
	defer file.Close()

	_, _ = io.Copy(w, file)
}

func (s *Server) handlePostUpload(w http.ResponseWriter, r *http.Request) {
	s.doUpload(w, r, uploadFilename)
}

func (s *Server) handlePostDatabase(w http.ResponseWriter, r *http.Request) {
	s.doUpload(w, r, dbFilename)
}

func (s *Server) doUpload(w http.ResponseWriter, r *http.Request, makeFilename func(bundleDir string, id int64) string) {
	defer r.Body.Close()

	targetFile, err := os.OpenFile(makeFilename(s.bundleDir, idFromRequest(r)), os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	if _, err := io.Copy(targetFile, r.Body); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleExists(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path := q.Get("path")

	if err := s.withDatabase(r, func(db *Database) error {
		exists, err := db.Exists(path)
		if err != nil {
			return err
		}

		_ = json.NewEncoder(w).Encode(exists)
		return nil
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

		_ = json.NewEncoder(w).Encode(locations)
		return nil
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

		_ = json.NewEncoder(w).Encode(locations)
		return nil
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

		_ = json.NewEncoder(w).Encode(resp)
		return nil
	}); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleMonikersByPosition(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path := q.Get("path")
	line, _ := strconv.Atoi(q.Get("line"))
	character, _ := strconv.Atoi(q.Get("character"))

	if err := s.withDatabase(r, func(db *Database) error {
		monikerData, err := db.MonikersByPosition(path, line, character)
		if err != nil {
			return err
		}

		_ = json.NewEncoder(w).Encode(monikerData)
		return nil
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
			Locations []Location `json:"locations"`
			Count     int        `json:"count"`
		}{
			Locations: locations,
			Count:     count,
		}

		_ = json.NewEncoder(w).Encode(resp)
		return nil
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

		_ = json.NewEncoder(w).Encode(resp)
		return nil
	}); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
}

func (s *Server) withDatabase(r *http.Request, handler func(db *Database) error) error {
	filename := dbFilename(s.bundleDir, idFromRequest(r))
	openDatabase := func() (*Database, error) { return OpenDatabase(filename, s.documentDataCache, s.resultChunkDataCache) }
	return s.databaseCache.WithDatabase(filename, openDatabase, handler)
}

func idFromRequest(r *http.Request) int64 {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	return id
}
