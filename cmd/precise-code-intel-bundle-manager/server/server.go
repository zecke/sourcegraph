package server

import (
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

func (s *Server) Handler() http.Handler {
	mux := mux.NewRouter()
	mux.HandleFunc("/uploads/{id:[0-9]+}", s.handleUpload)
	mux.HandleFunc("/dbs/{id:[0-9]+}", s.handleDatabase)
	mux.HandleFunc("/dbs/{id:[0-9]+}/exists", s.handleExists)
	mux.HandleFunc("/dbs/{id:[0-9]+}/definitions", s.handleDefinitions)
	mux.HandleFunc("/dbs/{id:[0-9]+}/references", s.handleReferences)
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
		_, _ = io.Copy(w, file) // TODO - handle error
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
	// TODO - configure limit
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		// TODO - handle error
		http.Error(w, "", http.StatusInternalServerError)
	}

	sourceFile, _, err := r.FormFile("file")
	if err != nil {
		// TODO - handle error
		http.Error(w, "", http.StatusInternalServerError)
	}
	defer sourceFile.Close()

	targetFile, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		// TODO - handle error
		http.Error(w, "", http.StatusInternalServerError)
	}

	_, _ = io.Copy(targetFile, sourceFile) // TODO - handle error
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleExists(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	db := NewDatabase(s.dbFilename(idFromRequest(r)))
	res, err := db.Exists()
	if err != nil {
		// TODO - handle error
		http.Error(w, "", http.StatusInternalServerError)
	}

	w.WriteHeader(http.StatusOK)
	w.Write(res)
}

func (s *Server) handleDefinitions(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	db := NewDatabase(s.dbFilename(idFromRequest(r)))
	res, err := db.Definitions()
	if err != nil {
		// TODO - handle error
		http.Error(w, "", http.StatusInternalServerError)
	}

	w.WriteHeader(http.StatusOK)
	w.Write(res)
}

func (s *Server) handleReferences(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	db := NewDatabase(s.dbFilename(idFromRequest(r)))
	res, err := db.References()
	if err != nil {
		// TODO - handle error
		http.Error(w, "", http.StatusInternalServerError)
	}

	w.WriteHeader(http.StatusOK)
	w.Write(res)
}

func (s *Server) handleMonikerByPosition(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	db := NewDatabase(s.dbFilename(idFromRequest(r)))
	res, err := db.MonikerByPosition()
	if err != nil {
		// TODO - handle error
		http.Error(w, "", http.StatusInternalServerError)
	}

	w.WriteHeader(http.StatusOK)
	w.Write(res)
}

func (s *Server) handleMonikerResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	db := NewDatabase(s.dbFilename(idFromRequest(r)))
	res, err := db.MonikerResults()
	if err != nil {
		// TODO - handle error
		http.Error(w, "", http.StatusInternalServerError)
	}

	w.WriteHeader(http.StatusOK)
	w.Write(res)
}

func (s *Server) handlePackageInformation(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	db := NewDatabase(s.dbFilename(idFromRequest(r)))
	res, err := db.PackageInformation()
	if err != nil {
		// TODO - handle error
		http.Error(w, "", http.StatusInternalServerError)
	}

	w.WriteHeader(http.StatusOK)
	w.Write(res)
}

func (s *Server) uploadFilename(id int64) string {
	return filepath.Join(s.StorageDir, "uploads", fmt.Sprintf("%d.lsif.gz", id))
}

func (s *Server) dbFilename(id int64) string {
	return s.dbFilename(id)
}

func idFromRequest(r *http.Request) int64 {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	return id
}
