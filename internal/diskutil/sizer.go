package diskutil

import (
	"syscall"

	"github.com/pkg/errors"
)

// DiskSizer gets information about disk size and free space.
type DiskSizer interface {
	BytesFreeOnDisk(mountPoint string) (uint64, error)
	DiskSizeBytes(mountPoint string) (uint64, error)
}

type StatDiskSizer struct{}

func (s *StatDiskSizer) BytesFreeOnDisk(mountPoint string) (uint64, error) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(mountPoint, &fs); err != nil {
		return 0, errors.Wrap(err, "statting")
	}
	free := fs.Bavail * uint64(fs.Bsize)
	return free, nil
}

func (s *StatDiskSizer) DiskSizeBytes(mountPoint string) (uint64, error) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(mountPoint, &fs); err != nil {
		return 0, errors.Wrap(err, "statting")
	}
	free := fs.Blocks * uint64(fs.Bsize)
	return free, nil
}
