package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type commandConfig struct {
	repository    string
	workspaceRoot string
	addr          string
	outputDir     string
}

func Run(ctx context.Context, args []string, version string) error {
	if len(args) == 0 {
		printRootHelp(os.Stdout)
		return nil
	}

	switch args[0] {
	case "help", "-h", "--help":
		printRootHelp(os.Stdout)
		return nil
	case "version", "--version", "-v":
		fmt.Println(version)
		return nil
	case "init":
		return runInit(args[1:])
	case "validate":
		return runValidate(args[1:])
	case "export":
		return runExport(args[1:])
	case "web":
		return runWeb(ctx, args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func defaultConfig() commandConfig {
	repo := os.Getenv("WORKTREEFOUNDRY_REPOSITORY")
	workspaceRoot := os.Getenv("WORKTREEFOUNDRY_WORKSPACE_ROOT")
	if workspaceRoot == "" {
		workspaceRoot = ".worktreefoundry/workspaces"
	}
	addr := os.Getenv("WORKTREEFOUNDRY_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	out := os.Getenv("WORKTREEFOUNDRY_OUT")
	if out == "" {
		out = "output"
	}
	return commandConfig{
		repository:    repo,
		workspaceRoot: workspaceRoot,
		addr:          addr,
		outputDir:     out,
	}
}

func runInit(args []string) error {
	cfg := defaultConfig()
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.repository, "repository", cfg.repository, "path to repository")
	sample := fs.Bool("sample", true, "populate sample schema and data")
	force := fs.Bool("force", false, "initialize even when directory exists")
	if err := fs.Parse(args); err != nil {
		return usageError("init", err)
	}
	if cfg.repository == "" {
		return errors.New("--repository is required (or WORKTREEFOUNDRY_REPOSITORY)")
	}
	return InitializeRepository(cfg.repository, *force, *sample)
}

func runValidate(args []string) error {
	cfg := defaultConfig()
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.repository, "repository", cfg.repository, "path to repository")
	if err := fs.Parse(args); err != nil {
		return usageError("validate", err)
	}
	if cfg.repository == "" {
		return errors.New("--repository is required (or WORKTREEFOUNDRY_REPOSITORY)")
	}

	repo, err := OpenRepository(cfg.repository, cfg.workspaceRoot)
	if err != nil {
		return err
	}
	result, err := ValidateRepository(repo.Root)
	if err != nil {
		return err
	}
	if !result.OK() {
		for _, issue := range result.Issues {
			fmt.Println(issue.String())
		}
		return fmt.Errorf("validation failed with %d issue(s)", len(result.Issues))
	}
	fmt.Println("validation passed")
	return nil
}

func runExport(args []string) error {
	cfg := defaultConfig()
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.repository, "repository", cfg.repository, "path to repository")
	fs.StringVar(&cfg.outputDir, "out", cfg.outputDir, "output path (absolute or relative to repository)")
	if err := fs.Parse(args); err != nil {
		return usageError("export", err)
	}
	if cfg.repository == "" {
		return errors.New("--repository is required (or WORKTREEFOUNDRY_REPOSITORY)")
	}

	repo, err := OpenRepository(cfg.repository, cfg.workspaceRoot)
	if err != nil {
		return err
	}
	outDir := cfg.outputDir
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(repo.Root, outDir)
	}
	if err := ExportRepository(repo.Root, outDir); err != nil {
		return err
	}
	fmt.Printf("export complete: %s\n", outDir)
	return nil
}

func runWeb(ctx context.Context, args []string) error {
	cfg := defaultConfig()
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.repository, "repository", cfg.repository, "path to repository")
	fs.StringVar(&cfg.workspaceRoot, "workspace-root", cfg.workspaceRoot, "workspace worktree root path (absolute or relative to repository)")
	fs.StringVar(&cfg.addr, "addr", cfg.addr, "bind address")
	if err := fs.Parse(args); err != nil {
		return usageError("web", err)
	}
	if cfg.repository == "" {
		return errors.New("--repository is required (or WORKTREEFOUNDRY_REPOSITORY)")
	}
	repo, err := OpenRepository(cfg.repository, cfg.workspaceRoot)
	if err != nil {
		return err
	}
	return StartWebServer(ctx, repo, cfg.addr)
}

func usageError(command string, err error) error {
	return fmt.Errorf("%w\n\n%s", err, commandUsage(command))
}

func printRootHelp(w io.Writer) {
	fmt.Fprint(w, `worktreefoundry manages schema-driven YAML configuration in a local Git repository.

Usage:
  worktreefoundry <command> [flags]

Commands:
  init      Initialize a repository with sample schema/data
  validate  Validate repository layout, objects, schema, and constraints
  export    Export deterministic JSON artifacts under output/
  web       Run the local web UI
  version   Print version

Environment variables:
  WORKTREEFOUNDRY_REPOSITORY
  WORKTREEFOUNDRY_WORKSPACE_ROOT
  WORKTREEFOUNDRY_ADDR
  WORKTREEFOUNDRY_OUT
`)
}

func commandUsage(command string) string {
	switch command {
	case "init":
		return "Usage: worktreefoundry init --repository /path/to/repo [--force]"
	case "validate":
		return "Usage: worktreefoundry validate --repository /path/to/repo"
	case "export":
		return "Usage: worktreefoundry export --repository /path/to/repo [--out output]"
	case "web":
		return "Usage: worktreefoundry web --repository /path/to/repo [--addr :8080] [--workspace-root .worktreefoundry/workspaces]"
	default:
		return ""
	}
}
