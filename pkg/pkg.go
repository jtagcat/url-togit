package pkg

import (
	"os"
	"path/filepath"
	"strings"
)

func PathResolveTilde(path string) (string, error) {
	if !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	dirname, err := os.UserHomeDir()
	return filepath.Join(dirname, path[2:]), err
}
