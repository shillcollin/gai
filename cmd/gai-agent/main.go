package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/shillcollin/gai/agentx"
	httpapi "github.com/shillcollin/gai/agentx/httpapi"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "run":
		if err := runCommand(args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "tail":
		if err := tailCommand(args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "resume":
		if err := resumeCommand(args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`gai-agent commands:
  run     --server URL --root PATH [--goal TEXT] [--result-view minimal|summary|full]
  tail    --server URL --root PATH
  resume  --server URL --root PATH`)
}

func runCommand(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	server := fs.String("server", "", "Base URL of the agent server")
	root := fs.String("root", "", "Task root directory")
	goal := fs.String("goal", "", "Task goal override")
	resultView := fs.String("result-view", "minimal", "Result view (minimal|summary|full)")
	specFile := fs.String("spec", "", "Optional path to JSON TaskSpec")
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *server == "" || *root == "" {
		return errors.New("server and root are required")
	}

	spec := agentx.TaskSpec{}
	if *specFile != "" {
		data, err := os.ReadFile(*specFile)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &spec); err != nil {
			return err
		}
	}
	if *goal != "" {
		spec.Goal = *goal
	}
	if spec.Goal == "" {
		return errors.New("task goal must be specified via --goal or spec file")
	}

	reqBody := httpapi.DoTaskRequest{Root: *root, Spec: spec, Options: httpapi.DoTaskRequestOpts{ResultView: *resultView}}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	endpoint := strings.TrimRight(*server, "/") + "/agent/tasks/do"
	resp, err := http.Post(endpoint, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, body)
	}
	var result agentx.DoTaskResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	fmt.Printf("Task %s completed in %v. Finish reason: %s\n", result.TaskID, time.Duration(result.CompletedAt-result.StartedAt)*time.Millisecond, result.FinishReason.Type)
	if result.OutputText != "" {
		fmt.Printf("Output:\n%s\n", result.OutputText)
	}
	return nil
}

func tailCommand(args []string) error {
	fs := flag.NewFlagSet("tail", flag.ContinueOnError)
	server := fs.String("server", "", "Base URL of the agent server")
	root := fs.String("root", "", "Task root directory")
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *server == "" || *root == "" {
		return errors.New("server and root are required")
	}

	endpoint := fmt.Sprintf("%s/agent/tasks/events?root=%s", strings.TrimRight(*server, "/"), *root)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, body)
	}
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			fmt.Print(string(line))
		}
		if errors.Is(err, io.EOF) {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if err != nil {
			return err
		}
	}
}

func resumeCommand(args []string) error {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	server := fs.String("server", "", "Base URL of the agent server")
	root := fs.String("root", "", "Task root directory")
	goal := fs.String("goal", "", "Task goal override")
	specFile := fs.String("spec", "", "Optional path to JSON TaskSpec")
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *server == "" || *root == "" {
		return errors.New("server and root are required")
	}

	spec := agentx.TaskSpec{}
	if *specFile != "" {
		data, err := os.ReadFile(*specFile)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &spec); err != nil {
			return err
		}
	}
	if *goal != "" {
		spec.Goal = *goal
	}
	if spec.Goal == "" {
		return errors.New("task goal must be specified via --goal or spec file")
	}

	req := httpapi.DoTaskRequest{Root: *root, Spec: spec}
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(*server, "/") + "/agent/tasks/do/async"
	resp, err := http.Post(endpoint, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, body)
	}
	var async asyncResponse
	if err := json.NewDecoder(resp.Body).Decode(&async); err != nil {
		return err
	}
	fmt.Printf("Resumed task job %s\n", async.JobID)
	return nil
}

type asyncResponse struct {
	JobID  string `json:"job_id"`
	TaskID string `json:"task_id"`
}
