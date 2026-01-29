//go:build !windows

package files

func isHiddenFile(filename string) (bool, error) {
	return filename[0] == '.' && len(filename) > 1, nil
}
