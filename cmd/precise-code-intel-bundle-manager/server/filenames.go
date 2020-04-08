package server

import (
	"fmt"
	"os"
	"path/filepath"
)

// PrepDirectories
func PrepDirectories(bundleDir string) error {
	for _, dir := range []string{uploadsDir(bundleDir), dbsDir(bundleDir)} {
		if err := os.MkdirAll(filepath.Join(bundleDir, dir), os.ModePerm); err != nil {
			return err
		}
	}

	return nil
}

// uploadsDir returns the path of the uploads directory.
func uploadsDir(bundleDir string) string {
	return filepath.Join(bundleDir, "uploads")
}

// dbsDir returns the path of the dbs directory.
func dbsDir(bundleDir string) string {
	return filepath.Join(bundleDir, "dbs")
}

// uploadFilename returns the path fo the upload with the given identifier.
func uploadFilename(bundleDir string, id int64) string {
	return filepath.Join(bundleDir, "uploads", fmt.Sprintf("%d.lsif.gz", id))
}

// dbFilename returns the path fo the database with the given identifier.
func dbFilename(bundleDir string, id int64) string {
	return filepath.Join(bundleDir, "dbs", fmt.Sprintf("%d.lsif.db", id))
}
