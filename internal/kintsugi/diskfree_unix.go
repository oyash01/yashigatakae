//go:build darwin || linux || freebsd || openbsd || netbsd

package kintsugi

import "syscall"

// diskFree returns (free, total) bytes for the filesystem holding `path`.
func diskFree(path string) (uint64, uint64, error) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil {
		return 0, 0, err
	}
	bsize := uint64(s.Bsize)
	free := uint64(s.Bavail) * bsize
	total := uint64(s.Blocks) * bsize
	return free, total, nil
}
