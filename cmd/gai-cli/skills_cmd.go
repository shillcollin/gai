package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/shillcollin/gai/skills"
)

func handleSkillCommands(ctx context.Context, args []string) (bool, error) {
	if len(args) == 0 || args[0] != "skills" {
		return false, nil
	}
	if len(args) == 1 {
		return true, errors.New("skills subcommand required (list|export)")
	}
	switch args[1] {
	case "list":
		return true, runSkillsList(args[2:])
	case "export":
		return true, runSkillsExport(args[2:])
	default:
		return true, fmt.Errorf("unknown skills subcommand %q", args[1])
	}
}

func runSkillsList(args []string) error {
	fs := flag.NewFlagSet("skills list", flag.ContinueOnError)
	glob := fs.String("glob", strings.Join(skills.DefaultManifestGlob(), ","), "Comma-separated glob patterns for skill manifests")
	if err := fs.Parse(args); err != nil {
		return err
	}
	patterns := strings.Split(*glob, ",")
	paths, err := skills.ExpandGlobs(patterns)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		fmt.Println("No skill manifests found")
		return nil
	}
	for _, path := range paths {
		manifest, err := skills.LoadManifest(path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		fmt.Printf("%s@%s — %s\n", manifest.Name, manifest.Version, manifest.Summary)
	}
	return nil
}

func runSkillsExport(args []string) error {
	fs := flag.NewFlagSet("skills export", flag.ContinueOnError)
	indent := fs.Bool("indent", true, "Pretty-print JSON output")
	outPath := fs.String("out", "", "Optional output file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	remaining := fs.Args()
	if len(remaining) == 0 {
		return errors.New("skills export requires a manifest path")
	}
	manifest, err := skills.LoadManifest(remaining[0])
	if err != nil {
		return err
	}
	skill, err := skills.New(manifest)
	if err != nil {
		return err
	}
	descriptor := skill.AnthropicDescriptor()
	var writer *os.File
	if *outPath == "" {
		writer = os.Stdout
	} else {
		file, err := os.Create(*outPath)
		if err != nil {
			return err
		}
		defer file.Close()
		writer = file
	}
	encoder := json.NewEncoder(writer)
	if *indent {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(descriptor)
}
