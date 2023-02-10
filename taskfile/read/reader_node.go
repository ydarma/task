package read

import (
	"os"
	"path/filepath"

	"github.com/go-task/task/v3/internal/filepathext"
)

type (
	readerNodeOption func(*readerNode)
	readerNode       struct {
		dir        string
		entrypoint string
		optional   bool
		parent     *readerNode
	}
)

func newReaderNode(
	dir string,
	entrypoint string,
	opts ...readerNodeOption,
) (*readerNode, error) {
	node := &readerNode{
		dir:        dir,
		entrypoint: entrypoint,
		optional:   false,
		parent:     nil,
	}

	// Apply options
	for _, opt := range opts {
		opt(node)
	}

	// If no directory is given, use the current working directory
	if node.dir == "" {
		d, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		node.dir = d
	}

	// If no entrypoint is given, search for a Taskfile recursively
	var path string
	if node.entrypoint == "" {
		var err error
		if path, err = findWalk(node.dir); err != nil {
			return nil, err
		}
		node.dir = filepath.Dir(path)
		node.entrypoint = filepath.Base(path)
	}

	return node, nil
}

func (node *readerNode) path() string {
	return filepathext.SmartJoin(node.dir, node.entrypoint)
}

func withParent(parent *readerNode) readerNodeOption {
	return func(node *readerNode) {
		node.parent = parent
	}
}

func withOptional(optional bool) readerNodeOption {
	return func(node *readerNode) {
		node.optional = optional
	}
}
