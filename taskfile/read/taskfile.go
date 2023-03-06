package read

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/go-task/task/v3/internal/execext"
	"github.com/go-task/task/v3/internal/filepathext"
	"github.com/go-task/task/v3/internal/sysinfo"
	"github.com/go-task/task/v3/taskfile"
)

var (
	defaultTaskfiles = []string{
		"Taskfile.yml",
		"Taskfile.yaml",
		"Taskfile.dist.yml",
		"Taskfile.dist.yaml",
	}
)

func resolvePath(baseDir, path string) (string, error) {
	path, err := execext.Expand(path)
	if err != nil {
		return "", err
	}

	if filepath.IsAbs(path) {
		return path, nil
	}

	result, err := filepath.Abs(filepathext.SmartJoin(baseDir, path))
	if err != nil {
		return "", fmt.Errorf("task: error resolving path %s relative to %s: %w", path, baseDir, err)
	}

	return result, nil
}

func readTaskfile(file string) (*taskfile.Taskfile, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var t taskfile.Taskfile
	if err := yaml.NewDecoder(f).Decode(&t); err != nil {
		return nil, fmt.Errorf("task: Failed to parse %s:\n%w", filepathext.TryAbsToRel(file), err)
	}

	return &t, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, os.ErrNotExist)
}

func find(path string) (string, error) {
	fi, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if fi.Mode().IsRegular() {
		return path, nil
	}

	for _, n := range defaultTaskfiles {
		fpath := filepathext.SmartJoin(path, n)
		if _, err := os.Stat(fpath); !errors.Is(err, os.ErrNotExist) {
			return fpath, nil
		}
	}

	return "", fmt.Errorf(`task: No Taskfile found in "%s". Use "task --init" to create a new one`, path)
}

func findWalk(path string) (string, error) {
	origPath := path
	owner, err := sysinfo.Owner(path)
	if err != nil {
		return "", err
	}
	for {
		fpath, err := find(path)
		if err == nil {
			return fpath, nil
		}

		// Get the parent path/user id
		parentPath := filepath.Dir(path)
		parentOwner, err := sysinfo.Owner(parentPath)
		if err != nil {
			return "", err
		}

		// Error if we reached the root directory and still haven't found a file
		// OR if the user id of the directory changes
		if path == parentPath || (parentOwner != owner) {
			return "", fmt.Errorf(`task: No Taskfile found in "%s" (or any of the parent directories). Use "task --init" to create a new one`, origPath)
		}

		owner = parentOwner
		path = parentPath
	}
}
