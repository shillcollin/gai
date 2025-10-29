package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/shillcollin/gai/agent"
)

func handleAgentCommands(ctx context.Context, args []string) (bool, error) {
	if len(args) == 0 || args[0] != "agent" {
		return false, nil
	}
	if len(args) == 1 {
		return true, errors.New("agent subcommand required (inspect|skills)")
	}
	sub := args[1]
	switch sub {
	case "inspect":
		return true, runAgentInspect(args[2:])
	case "skills":
		return true, runAgentSkills(args[2:])
	default:
		return true, fmt.Errorf("unknown agent subcommand %q", sub)
	}
}

func runAgentInspect(args []string) error {
	fs := flag.NewFlagSet("agent inspect", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	remaining := fs.Args()
	if len(remaining) == 0 {
		return errors.New("agent inspect requires a bundle directory")
	}
	bundle, err := agent.LoadBundle(remaining[0])
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "Bundle:\t%s\n", bundle.Manifest.Name)
	fmt.Fprintf(w, "Version:\t%s\n", bundle.Manifest.Version)
	fmt.Fprintf(w, "Default Skill:\t%s\n", bundle.Manifest.DefaultSkill)
	fmt.Fprintf(w, "Max Steps:\t%d\n", bundle.Manifest.MaxSteps)
	if bundle.SharedWorkspacePath() != "" {
		fmt.Fprintf(w, "Shared Workspace:\t%s\n", bundle.SharedWorkspacePath())
	}
	fmt.Fprintf(w, "Skills:\t%s\n", strings.Join(bundle.SkillNames(), ", "))
	return w.Flush()
}

func runAgentSkills(args []string) error {
	fs := flag.NewFlagSet("agent skills", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	remaining := fs.Args()
	if len(remaining) == 0 {
		return errors.New("agent skills requires a bundle directory")
	}
	bundle, err := agent.LoadBundle(remaining[0])
	if err != nil {
		return err
	}
	names := bundle.SkillNames()
	if len(names) == 0 {
		fmt.Println("(no skills found)")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "NAME\tVERSION\tSUMMARY\n")
	for _, name := range names {
		cfg, _ := bundle.SkillConfig(name)
		fmt.Fprintf(w, "%s\t%s\t%s\n", name, cfg.Skill.Manifest.Version, cfg.Skill.Manifest.Summary)
	}
	return w.Flush()
}
