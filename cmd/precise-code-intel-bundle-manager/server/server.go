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
	StorageDir string
}

// TODO
// github.com/golang/groupcache/lru

func (s *Server) Handler() http.Handler {
	mux := mux.NewRouter()
	mux.HandleFunc("/uploads/{id:[0-9]+}", s.handleUpload)
	mux.HandleFunc("/dbs/{id:[0-9]+}", s.handleDatabase)
	mux.HandleFunc("/dbs/{id:[0-9]+}/exists", s.handleExists)
	mux.HandleFunc("/dbs/{id:[0-9]+}/definitions", s.handleDefinitions)
	mux.HandleFunc("/dbs/{id:[0-9]+}/references", s.handleReferences)
	mux.HandleFunc("/dbs/{id:[0-9]+}/hover", s.handleHover)
	mux.HandleFunc("/dbs/{id:[0-9]+}/monikersByPosition", s.handleMonikerByPosition)
	mux.HandleFunc("/dbs/{id:[0-9]+}/monikerResults", s.handleMonikerResults)
	mux.HandleFunc("/dbs/{id:[0-9]+}/packageInformation", s.handlePackageInformation)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		file, err := os.Open(s.uploadFilename(idFromRequest(r)))
		if err != nil {
			http.Error(w, "Upload not found.", http.StatusNotFound)
			return
		}
		defer file.Close()

		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, file)
		return
	}

	if r.Method == "POST" {
		s.doUpload(w, r, s.uploadFilename(idFromRequest(r)))
		return
	}

	w.WriteHeader(http.StatusMethodNotAllowed)
}

func (s *Server) handleDatabase(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	s.doUpload(w, r, s.dbFilename(idFromRequest(r)))
}

func (s *Server) doUpload(w http.ResponseWriter, r *http.Request, filename string) {
	defer r.Body.Close()

	targetFile, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	_, _ = io.Copy(targetFile, r.Body)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) withDatabase(w http.ResponseWriter, r *http.Request, handler func(db *Database) error) {
	db, err := OpenDatabase(s.dbFilename(idFromRequest(r)))
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer db.Close()

	if err := handler(db); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleExists(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Query().Get("path")

	s.withDatabase(w, r, func(db *Database) error {
		exists, err := db.Exists(path)
		if err != nil {
			return err
		}

		return json.NewEncoder(w).Encode(exists)
	})

}

func (s *Server) handleDefinitions(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	path := q.Get("path")
	line, _ := strconv.Atoi(q.Get("line"))
	character, _ := strconv.Atoi(q.Get("character"))

	s.withDatabase(w, r, func(db *Database) error {
		locations, err := db.Definitions(path, line, character)
		if err != nil {
			return err
		}

		return json.NewEncoder(w).Encode(locations)
	})
}

func (s *Server) handleReferences(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	path := q.Get("path")
	line, _ := strconv.Atoi(q.Get("line"))
	character, _ := strconv.Atoi(q.Get("character"))

	s.withDatabase(w, r, func(db *Database) error {
		locations, err := db.References(path, line, character)
		if err != nil {
			return err
		}

		return json.NewEncoder(w).Encode(locations)
	})
}

func (s *Server) handleHover(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	path := q.Get("path")
	line, _ := strconv.Atoi(q.Get("line"))
	character, _ := strconv.Atoi(q.Get("character"))

	s.withDatabase(w, r, func(db *Database) error {
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
	})
}

func (s *Server) handleMonikerByPosition(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	path := q.Get("path")
	line, _ := strconv.Atoi(q.Get("line"))
	character, _ := strconv.Atoi(q.Get("character"))

	s.withDatabase(w, r, func(db *Database) error {
		monikerData, err := db.MonikerByPosition(path, line, character)
		if err != nil {
			return err
		}

		return json.NewEncoder(w).Encode(monikerData)
	})
}

func (s *Server) handleMonikerResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	modelType := q.Get("modelType")
	scheme := q.Get("scheme")
	identifier := q.Get("identifier")

	skip, err1 := strconv.Atoi(q.Get("skip"))
	if err1 != nil {
		skip = 0
	}

	take, err2 := strconv.Atoi(q.Get("take"))
	if err2 != nil {
		take = 100
	}

	var tableName string
	if modelType == "definition" {
		tableName = "definitions"
	} else {
		tableName = "references"
	}

	s.withDatabase(w, r, func(db *Database) error {
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
	})
}

func (s *Server) handlePackageInformation(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	path := q.Get("path")
	packageInformationID := ID(q.Get("packageInformationId"))

	s.withDatabase(w, r, func(db *Database) error {
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
	})
}

func (s *Server) uploadFilename(id int64) string {
	return filepath.Join(s.StorageDir, "uploads", fmt.Sprintf("%d.lsif.gz", id))
}

func (s *Server) dbFilename(id int64) string {
	return filepath.Join(s.StorageDir, "dbs", fmt.Sprintf("%d.lsif.db", id))
}

func idFromRequest(r *http.Request) int64 {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	return id
}
