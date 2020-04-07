package diskutil

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/pkg/errors"
)

// FindMountPoint searches upwards starting from the directory d to find the mount point.
func FindMountPoint(d string) (string, error) {
	d, err := filepath.Abs(d)
	if err != nil {
		return "", errors.Wrapf(err, "getting absolute version of %s", d)
	}
	for {
		m, err := isMount(d)
		if err != nil {
			return "", errors.Wrapf(err, "finding out if %s is a mount point", d)
		}
		if m {
			return d, nil
		}
		d2 := filepath.Dir(d)
		if d2 == d {
			return d2, nil
		}
		d = d2
	}
}

// isMount tells whether the directory d is a mount point.
func isMount(d string) (bool, error) {
	ddev, err := device(d)
	if err != nil {
		return false, errors.Wrapf(err, "gettting device id for %s", d)
	}
	parent := filepath.Dir(d)
	if parent == d {
		return true, nil
	}
	pdev, err := device(parent)
	if err != nil {
		return false, errors.Wrapf(err, "getting device id for %s", parent)
	}
	return pdev != ddev, nil
}

// device gets the device id of a file f.
func device(f string) (int64, error) {
	fi, err := os.Stat(f)
	if err != nil {
		return 0, errors.Wrapf(err, "running stat on %s", f)
	}
	stat, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("failed to get stat details for %s", f)
	}
	return int64(stat.Dev), nil
}
