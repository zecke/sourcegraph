package server

import (
	"fmt"
	"path/filepath"
)

func uploadsDir(bundleDir string) string {
	return filepath.Join(bundleDir, "uploads")
}

func dbsDir(bundleDir string) string {
	return filepath.Join(bundleDir, "dbs")
}

func uploadFilename(bundleDir string, id int64) string {
	return filepath.Join(bundleDir, "uploads", fmt.Sprintf("%d.lsif.gz", id))
}

func dbFilename(bundleDir string, id int64) string {
	return filepath.Join(bundleDir, "dbs", fmt.Sprintf("%d.lsif.db", id))
}
