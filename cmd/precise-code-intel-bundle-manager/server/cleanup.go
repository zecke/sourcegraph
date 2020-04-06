package server

import (
	"context"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sourcegraph/sourcegraph/internal/codeintel/lsifserver/client"
	"github.com/sourcegraph/sourcegraph/internal/env"
)

var (
	DirSizeMB          = env.Get("DBS_DIR_MAXIMUM_SIZE_MB", "10000", "maximum space that the dbs directory can use in megabytes")
	FailedUploadMaxAge = env.Get("FAILED_UPLOAD_MAX_AGE", "86400", "maximum age that the files for an unprocessed upload can remain on disk in seconds")
	DeadDumpBatchSize  = env.Get("DEAD_DUMP_BATCH_SIZE", "100", "how many uploads to query at once when determining if a db file is unreferenced")
)

func (s *Server) Janitor() error {
	for _, task := range []func() error{s.removeDeadDumps, s.cleanOldDumps, s.cleanFailedUploads} {
		if err := task(); err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) cleanOldDumps() error {
	var maxDirSizeBytes int64
	if i, err := strconv.ParseInt(DirSizeMB, 10, 64); err != nil {
		log.Fatalf("invalid int %q for DBS_DIR_MAXIMUM_SIZE_MB: %s", DirSizeMB, err)
	} else {
		maxDirSizeBytes = i * 1000 * 1000
	}

	dirSizeBytes, err := dirSize(filepath.Join(s.storageDir, "dbs"))
	if err != nil {
		return err
	}

	for dirSizeBytes > maxDirSizeBytes {
		id, prunable, err := client.DefaultClient.Prune(context.Background())
		if err != nil {
			return err
		}

		if !prunable {
			break
		}

		filename := s.dbFilename(id)
		fileInfo, err := os.Stat(filename)
		if err != nil {
			return err
		}

		if err := os.Remove(filename); err != nil {
			return err
		}

		dirSizeBytes -= fileInfo.Size()
	}

	return nil
}

func (s *Server) removeDeadDumps() error {
	var deadDumpBatchSize int64
	if i, err := strconv.ParseInt(DeadDumpBatchSize, 10, 64); err != nil {
		log.Fatalf("invalid int %q for DEAD_DUMP_BATCH_SIZE: %s", DeadDumpBatchSize, err)
	} else {
		deadDumpBatchSize = i
	}

	fileInfos, err := ioutil.ReadDir(filepath.Join(s.storageDir, "dbs"))
	if err != nil {
		return err
	}

	pathsByID := map[int]string{}
	for _, fileInfo := range fileInfos {
		id, ok := idFromFilename(fileInfo.Name())
		if !ok {
			continue
		}

		pathsByID[id] = filepath.Join(s.storageDir, "dbs", fileInfo.Name())
	}

	var ids []int
	for id := range pathsByID {
		ids = append(ids, id)
	}

	allStates := map[int]string{}
	for len(ids) > 0 {
		batchSize := int(deadDumpBatchSize)
		if len(ids) < batchSize {
			batchSize = len(ids)
		}

		states, err := client.DefaultClient.States(context.Background(), ids[:batchSize])
		if err != nil {
			return err
		}
		for k, v := range states {
			allStates[k] = v
		}

		ids = ids[deadDumpBatchSize:]
	}

	for id, path := range pathsByID {
		state, exists := allStates[id]
		if !exists || state == "errored" {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Server) cleanFailedUploads() error {
	var maxAge time.Duration
	if i, err := strconv.ParseInt(FailedUploadMaxAge, 10, 64); err != nil {
		log.Fatalf("invalid int %q for FAILED_UPLOAD_MAX_AGE: %s", FailedUploadMaxAge, err)
	} else {
		maxAge = time.Duration(i) * time.Second
	}

	fileInfos, err := ioutil.ReadDir(filepath.Join(s.storageDir, "uploads"))
	if err != nil {
		return err
	}

	for _, fileInfo := range fileInfos {
		if time.Since(fileInfo.ModTime()) < maxAge {
			continue
		}

		if err := os.Remove(filepath.Join(s.storageDir, "uploads", fileInfo.Name())); err != nil {
			return err
		}
	}

	return nil
}

func idFromFilename(filename string) (int, bool) {
	id, err := strconv.Atoi(strings.Split(filename, ".")[0])
	if err != nil {
		return 0, false
	}

	return int(id), true
}

func dirSize(path string) (int64, error) {
	fileInfos, err := ioutil.ReadDir(path)
	if err != nil {
		return 0, err
	}

	size := int64(0)
	for _, fileInfo := range fileInfos {
		if !fileInfo.IsDir() {
			size += fileInfo.Size()
		}
	}

	return size, nil
}
