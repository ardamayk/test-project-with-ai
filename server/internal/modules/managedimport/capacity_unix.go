//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris || zos

package managedimport

import (
	"fmt"
	"math"

	"golang.org/x/sys/unix"
)

func availableStorageBytes(path string) (int64, error) {
	var statistics unix.Statfs_t
	if err := unix.Statfs(path, &statistics); err != nil {
		return 0, fmt.Errorf("stat filesystem %q: %w", path, err)
	}
	blockSize := uint64(statistics.Bsize)
	availableBlocks := uint64(statistics.Bavail)
	if blockSize != 0 && availableBlocks > math.MaxInt64/blockSize {
		return math.MaxInt64, nil
	}
	return int64(availableBlocks * blockSize), nil
}
