package agentx

import (
	"path/filepath"
)

// TaskEnv provides structured access to task directory paths.
type TaskEnv struct {
	root string
}

// Task returns the path to a file/directory within the task inputs directory.
func (t TaskEnv) Task(path string) string {
	if path == "." || path == "" {
		return filepath.Join(t.root, "task")
	}
	return filepath.Join(t.root, "task", path)
}

// Progress returns the path to a file/directory within the progress directory.
func (t TaskEnv) Progress(path string) string {
	if path == "." || path == "" {
		return filepath.Join(t.root, "progress")
	}
	return filepath.Join(t.root, "progress", path)
}

// Root returns the task root directory.
func (t TaskEnv) Root() string {
	return t.root
}
