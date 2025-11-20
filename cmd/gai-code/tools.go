package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shillcollin/gai/core"
	"github.com/shillcollin/gai/tools"
)

// -- File System Tools --

type readFileInput struct {
	Path string `json:"path" description:"Path to the file to read"`
}

type readFileOutput struct {
	Content string `json:"content"`
}

func newReadFileTool() core.ToolHandle {
	return tools.NewCoreAdapter(tools.New[readFileInput, readFileOutput](
		"read_file",
		"Read the contents of a file at the given path.",
		func(ctx context.Context, in readFileInput, meta core.ToolMeta) (readFileOutput, error) {
			data, err := os.ReadFile(in.Path)
			if err != nil {
				return readFileOutput{}, fmt.Errorf("read file: %w", err)
			}
			return readFileOutput{Content: string(data)}, nil
		},
	))
}

type writeFileInput struct {
	Path    string `json:"path" description:"Path to the file to write"`
	Content string `json:"content" description:"Content to write to the file"`
}

type writeFileOutput struct {
	Success bool `json:"success"`
}

func newWriteFileTool() core.ToolHandle {
	return tools.NewCoreAdapter(tools.New[writeFileInput, writeFileOutput](
		"write_file",
		"Write content to a file at the given path. Overwrites if exists.",
		func(ctx context.Context, in writeFileInput, meta core.ToolMeta) (writeFileOutput, error) {
			if err := os.MkdirAll(filepath.Dir(in.Path), 0755); err != nil {
				return writeFileOutput{}, fmt.Errorf("create dir: %w", err)
			}
			if err := os.WriteFile(in.Path, []byte(in.Content), 0644); err != nil {
				return writeFileOutput{}, fmt.Errorf("write file: %w", err)
			}
			return writeFileOutput{Success: true}, nil
		},
	))
}

type listDirInput struct {
	Path string `json:"path" description:"Path to the directory to list"`
}

type listDirOutput struct {
	Entries []string `json:"entries"`
}

func newListDirTool() core.ToolHandle {
	return tools.NewCoreAdapter(tools.New[listDirInput, listDirOutput](
		"list_directory",
		"List files and directories in the given path.",
		func(ctx context.Context, in listDirInput, meta core.ToolMeta) (listDirOutput, error) {
			entries, err := os.ReadDir(in.Path)
			if err != nil {
				return listDirOutput{}, fmt.Errorf("read dir: %w", err)
			}
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				suffix := ""
				if e.IsDir() {
					suffix = "/"
				}
				names = append(names, e.Name()+suffix)
			}
			return listDirOutput{Entries: names}, nil
		},
	))
}

// -- Shell Tool --

type runCommandInput struct {
	Command string `json:"command" description:"Shell command to execute"`
}

type runCommandOutput struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

func newRunCommandTool() core.ToolHandle {
	return tools.NewCoreAdapter(tools.New[runCommandInput, runCommandOutput](
		"run_command",
		"Execute a shell command on the local machine. Use this to run git, tests, etc.",
		func(ctx context.Context, in runCommandInput, meta core.ToolMeta) (runCommandOutput, error) {
			cmd := exec.CommandContext(ctx, "/bin/sh", "-c", in.Command)
			var stdout, stderr strings.Builder
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			exitCode := 0
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				} else {
					// Other error (e.g. context cancelled or not found)
					stderr.WriteString(fmt.Sprintf("\nExecution error: %v", err))
					exitCode = 1
				}
			}
			return runCommandOutput{
				Stdout:   stdout.String(),
				Stderr:   stderr.String(),
				ExitCode: exitCode,
			}, nil
		},
	))
}
