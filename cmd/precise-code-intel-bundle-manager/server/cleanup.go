package server

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sourcegraph/sourcegraph/internal/codeintel/lsifserver/client"
	"github.com/sourcegraph/sourcegraph/internal/diskutil"
)

const DeadDumpBatchSize = 100

func (s *Server) Janitor() error {
	for _, fn := range []func() error{s.cleanFailedUploads, s.removeDeadDumps, s.freeSpace} {
		if err := fn(); err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) cleanFailedUploads() error {
	fileInfos, err := ioutil.ReadDir(filepath.Join(s.bundleDir, "uploads"))
	if err != nil {
		return err
	}

	for _, fileInfo := range fileInfos {
		if time.Since(fileInfo.ModTime()) < s.maxUnconvertedUploadAge {
			continue
		}

		if err := os.Remove(filepath.Join(s.bundleDir, "uploads", fileInfo.Name())); err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) removeDeadDumps() error {
	pathsByID, err := s.databasePathsByID()
	if err != nil {
		return err
	}

	var ids []int
	for id := range pathsByID {
		ids = append(ids, id)
	}

	allStates := map[int]string{}
	for _, batch := range batchSlice(ids, DeadDumpBatchSize) {
		states, err := client.DefaultClient.States(context.Background(), batch)
		if err != nil {
			return err
		}

		for k, v := range states {
			allStates[k] = v
		}
	}

	for id, path := range pathsByID {
		if state, exists := allStates[id]; !exists || state == "errored" {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Server) databasePathsByID() (map[int]string, error) {
	fileInfos, err := ioutil.ReadDir(filepath.Join(s.bundleDir, "dbs"))
	if err != nil {
		return nil, err
	}

	pathsByID := map[int]string{}
	for _, fileInfo := range fileInfos {
		if id, err := strconv.Atoi(strings.Split(fileInfo.Name(), ".")[0]); err == nil {
			pathsByID[int(id)] = filepath.Join(s.bundleDir, "dbs", fileInfo.Name())
		}
	}

	return pathsByID, nil
}

func (s *Server) freeSpace() error {
	bytesToFree, err := s.bytesToFree()
	if err != nil || bytesToFree == 0 {
		return err
	}

	return s.cleanOldDumps(bytesToFree)
}

func (s *Server) bytesToFree() (uint64, error) {
	diskSizeBytes, freeBytes, err := s.diskSize()
	if err != nil {
		return 0, err
	}

	desiredFreeBytes := uint64(float64(diskSizeBytes) * float64(s.desiredPercentFree) / 100.0)
	if desiredFreeBytes < freeBytes {
		return 0, nil
	}

	return uint64(desiredFreeBytes - freeBytes), nil
}

func (s *Server) diskSize() (uint64, uint64, error) {
	if s.diskSizer == nil {
		diskSizer, err := diskutil.NewDiskSizer(s.bundleDir)
		if err != nil {
			return 0, 0, err
		}

		s.diskSizer = diskSizer
	}

	return s.diskSizer.Size()
}

func (s *Server) cleanOldDumps(bytesToFree uint64) error {
	for bytesToFree > 0 {
		bytesRemoved, pruned, err := s.cleanOldDump()
		if err != nil {
			return err
		}
		if !pruned {
			break
		}

		if bytesRemoved >= bytesToFree {
			break
		}

		bytesToFree -= bytesRemoved
	}

	return nil
}

func (s *Server) cleanOldDump() (uint64, bool, error) {
	id, prunable, err := client.DefaultClient.Prune(context.Background())
	if err != nil || !prunable {
		return 0, false, err
	}

	filename := s.dbFilename(id)
	fileSize, err := filesize(filename)
	if err != nil {
		return 0, false, err
	}

	if err := os.Remove(filename); err != nil {
		return 0, false, err
	}

	return fileSize, true, nil
}

func batchSlice(s []int, batchSize int) [][]int {
	batches := [][]int{}
	for len(s) > batchSize {
		batches = append(batches, s[:batchSize])
		s = s[batchSize:]
	}

	if len(s) > 0 {
		batches = append(batches, s)
	}

	return batches
}

func filesize(filename string) (uint64, error) {
	fileInfo, err := os.Stat(filename)
	if err != nil {
		return 0, err
	}

	return uint64(fileInfo.Size()), nil
}
