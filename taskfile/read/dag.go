package read

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dominikbraun/graph"
	"github.com/dominikbraun/graph/draw"
	"golang.org/x/sync/errgroup"

	"github.com/go-task/task/v3/internal/templater"
	"github.com/go-task/task/v3/taskfile"
)

// TaskfileDAG is a directed acyclic graph of Taskfiles.
type TaskfileDAG struct {
	graph.Graph[string, *TaskfileDAGVertex]
}

// A TaskfileDAGVertex is a vertex on the TaskfileDAG.
type TaskfileDAGVertex struct {
	path     string
	taskfile *taskfile.Taskfile
}

func taskfileHash(vertex *TaskfileDAGVertex) string {
	return vertex.path
}

func NewTaskfileDAG(dir, entrypoint string) (*TaskfileDAG, error) {
	dag := &TaskfileDAG{
		Graph: graph.New(taskfileHash,
			graph.Directed(),
			graph.PreventCycles(),
			graph.Rooted(),
		),
	}

	// Create a new reader node
	node, err := newReaderNode(dir, entrypoint)
	if err != nil {
		return nil, err
	}

	// Recursively loop through each Taskfile, adding vertices/edges to the graph
	if err := dag.addIncludedTaskfiles(node); err != nil {
		return nil, err
	}

	return dag, nil
}

func (dag *TaskfileDAG) Visualize(filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	return draw.DOT(dag.Graph, f)
}

func (dag *TaskfileDAG) addIncludedTaskfiles(node *readerNode) error {

	// Create a new vertex for the Taskfile
	vertex := &TaskfileDAGVertex{
		path:     node.path(),
		taskfile: nil,
	}

	// Add the included Taskfile to the DAG
	// If the vertex already exists, we return early since its Taskfile has
	// already been read and its children explored
	if err := dag.AddVertex(vertex); err == graph.ErrVertexAlreadyExists {
		return nil
	} else if err != nil {
		return err
	}

	// Read and parse the Taskfile from the file and add it to the vertex
	var err error
	vertex.taskfile, err = readTaskfile(node.path())
	if err != nil {
		if node.optional {
			return nil
		}
		return err
	}

	// TODO: can probably avoid this with the range function?
	// Return if there are no children
	if vertex.taskfile.Includes == nil {
		return nil
	}

	// Create an error group to wait for all included Taskfiles to be read
	var g errgroup.Group

	// Loop over each included taskfile
	for _, key := range vertex.taskfile.Includes.Keys {
		namespace := key

		// Get the map entry and skip if it doesn't exist
		includedTaskfile, ok := vertex.taskfile.Includes.Mapping[key]
		if !ok {
			continue
		}

		// Start a goroutine to process each included Taskfile
		g.Go(func() error {

			// If the Taskfile schema is v3 or higher, replace all variables with their values
			if vertex.taskfile.Version.Compare(taskfile.V3) >= 0 {
				tr := templater.Templater{Vars: vertex.taskfile.Vars, RemoveNoValue: true}
				includedTaskfile.Taskfile = tr.Replace(includedTaskfile.Taskfile)
				if err := tr.Err(); err != nil {
					return err
				}
			}

			// Get the full path to the included Taskfile
			// This is used as the hash for the node
			path, err := resolvePath(node.dir, includedTaskfile.Taskfile)
			if err != nil {
				return err
			}

			// Create a new reader node for the included Taskfile
			includedTaskfileNode, err := newReaderNode(
				filepath.Dir(path),
				filepath.Base(path),
				withParent(node),
				withOptional(includedTaskfile.Optional),
			)
			if err != nil {
				return err
			}

			// Recurse into the included Taskfile
			if err := dag.addIncludedTaskfiles(includedTaskfileNode); err != nil {
				return err
			}

			mergeOptions := &taskfile.MergeOptions{
				Namespace: namespace,
				Dir:       includedTaskfile.Dir,
				Internal:  includedTaskfile.Internal,
				Aliases:   includedTaskfile.Aliases,
				Vars:      includedTaskfile.Vars,
			}

			// Create an edge between the Taskfiles
			return dag.AddEdge(node.path(), includedTaskfileNode.path(), graph.EdgeData(mergeOptions))
		})
	}

	// Wait for all the go routines to finish
	return g.Wait()
}

func (dag *TaskfileDAG) Merge() (*taskfile.Taskfile, error) {

	hashes, err := graph.TopologicalSort(dag.Graph)
	if err != nil {
		return nil, err
	}

	predecessorMap, err := dag.PredecessorMap()
	if err != nil {
		return nil, err
	}

	for i := len(hashes) - 1; i >= 0; i-- {
		hash := hashes[i]

		// Get the current vertex
		vertex, err := dag.Vertex(hash)
		if err != nil {
			return nil, err
		}

		// Create an error group to wait for all the included Taskfiles to be merged with all its parents
		var g errgroup.Group

		// Loop over each adjacent edge
		for _, edge := range predecessorMap[hash] {

			// TODO: Enable goroutines
			// Start a goroutine to process each included Taskfile
			// g.Go(
			err := func() error {

				// Get the child vertex
				predecessorVertex, err := dag.Vertex(edge.Source)
				if err != nil {
					return err
				}

				// Get the merge options
				mergeOptions, ok := edge.Properties.Data.(*taskfile.MergeOptions)
				if !ok {
					return fmt.Errorf("task: Failed to get merge options")
				}

				// Merge the included Taskfile into the parent Taskfile
				if err := taskfile.Merge(
					predecessorVertex.taskfile,
					vertex.taskfile,
					mergeOptions,
				); err != nil {
					return err
				}

				return nil
			}()
			if err != nil {
				return nil, err
			}
			// )
		}

		// Wait for all the go routines to finish
		if err := g.Wait(); err != nil {
			return nil, err
		}
	}

	// Get the root vertex
	rootVertex, err := dag.Vertex(hashes[0])
	if err != nil {
		return nil, err
	}

	for name, task := range rootVertex.taskfile.Tasks {
		if task == nil {
			task = &taskfile.Task{}
			rootVertex.taskfile.Tasks[name] = task
		}
		task.Task = name
	}

	return rootVertex.taskfile, nil
}
