package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var workspaceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

type Repository struct {
	Root          string
	WorkspaceRoot string
	mu            sync.Mutex
}

type Workspace struct {
	Name         string
	Branch       string
	Path         string
	Dirty        bool
	ChangedFiles []string
}

type ChangedEntry struct {
	Path   string
	Status string
}

func OpenRepository(root, workspaceRoot string) (*Repository, error) {
	if root == "" {
		return nil, errors.New("repository root required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository path: %w", err)
	}
	st, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("stat repository: %w", err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("repository path is not a directory: %s", absRoot)
	}
	if _, err := os.Stat(filepath.Join(absRoot, ".git")); err != nil {
		return nil, fmt.Errorf("repository is not a git checkout: %s", absRoot)
	}
	wsRoot := workspaceRoot
	if wsRoot == "" {
		wsRoot = filepath.Join(absRoot, ".worktreefoundry", "workspaces")
	}
	if !filepath.IsAbs(wsRoot) {
		wsRoot = filepath.Join(absRoot, wsRoot)
	}
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create workspace root: %w", err)
	}
	return &Repository{Root: absRoot, WorkspaceRoot: wsRoot}, nil
}

func (r *Repository) BranchForWorkspace(name string) string {
	return "workspace/" + name
}

func (r *Repository) WorkspacePath(name string) string {
	return filepath.Join(r.WorkspaceRoot, name)
}

func (r *Repository) WorkspaceExists(name string) bool {
	_, err := os.Stat(r.WorkspacePath(name))
	return err == nil
}

func (r *Repository) CurrentBranch(repoPath string) (string, error) {
	out, err := r.runGit(repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (r *Repository) ListTypes(repoPath string) ([]string, error) {
	dataDir := filepath.Join(repoPath, "data")
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var types []string
	for _, e := range entries {
		if e.IsDir() {
			types = append(types, e.Name())
		}
	}
	sort.Strings(types)
	return types, nil
}

func (r *Repository) CreateWorkspace(name string) error {
	if !workspaceNamePattern.MatchString(name) {
		return fmt.Errorf("workspace name %q is invalid", name)
	}
	path := r.WorkspacePath(name)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("workspace %q already exists", name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := os.MkdirAll(r.WorkspaceRoot, 0o755); err != nil {
		return fmt.Errorf("create workspace root: %w", err)
	}
	_, err := r.runGit(r.Root, "worktree", "add", "-b", r.BranchForWorkspace(name), path, "main")
	if err != nil {
		return err
	}
	return nil
}

func (r *Repository) DeleteWorkspace(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.deleteWorkspaceLocked(name)
}

func (r *Repository) deleteWorkspaceLocked(name string) error {
	path := r.WorkspacePath(name)
	branch := r.BranchForWorkspace(name)

	if _, err := os.Stat(path); err == nil {
		if _, err := r.runGit(r.Root, "worktree", "remove", "--force", path); err != nil {
			return err
		}
	}
	if _, err := r.runGit(r.Root, "branch", "-D", branch); err != nil {
		if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "not exist") {
			return err
		}
	}
	return nil
}

func (r *Repository) ListWorkspaces() ([]Workspace, error) {
	out, err := r.runGit(r.Root, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	blocks := strings.Split(strings.TrimSpace(out), "\n\n")
	workspaces := make([]Workspace, 0)
	for _, block := range blocks {
		if strings.TrimSpace(block) == "" {
			continue
		}
		ws := Workspace{}
		lines := strings.Split(block, "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "worktree ") {
				ws.Path = strings.TrimPrefix(line, "worktree ")
			}
			if strings.HasPrefix(line, "branch ") {
				b := strings.TrimPrefix(line, "branch refs/heads/")
				ws.Branch = b
			}
		}
		if !strings.HasPrefix(ws.Branch, "workspace/") {
			continue
		}
		ws.Name = strings.TrimPrefix(ws.Branch, "workspace/")
		changed, err := r.ChangedFiles(ws.Path)
		if err == nil {
			ws.ChangedFiles = changed
			ws.Dirty = len(changed) > 0
		}
		workspaces = append(workspaces, ws)
	}
	sort.Slice(workspaces, func(i, j int) bool {
		return workspaces[i].Name < workspaces[j].Name
	})
	return workspaces, nil
}

func (r *Repository) ChangedFiles(repoPath string) ([]string, error) {
	entries, err := r.ChangedEntries(repoPath)
	if err != nil {
		return nil, err
	}
	changed := make([]string, 0, len(entries))
	for _, entry := range entries {
		changed = append(changed, entry.Path)
	}
	sort.Strings(changed)
	return changed, nil
}

func (r *Repository) ChangedEntries(repoPath string) ([]ChangedEntry, error) {
	out, err := r.runGit(repoPath, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var changed []ChangedEntry
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) < 4 {
			continue
		}
		statusToken := line[:2]
		path := strings.TrimSpace(line[3:])
		if strings.Contains(path, " -> ") {
			parts := strings.Split(path, " -> ")
			path = parts[len(parts)-1]
		}
		path = filepath.ToSlash(path)
		if isIgnoredAppPath(path) {
			continue
		}
		changed = append(changed, ChangedEntry{
			Path:   path,
			Status: statusFromToken(statusToken),
		})
	}
	return changed, nil
}

func isIgnoredAppPath(path string) bool {
	return strings.HasPrefix(path, ".worktreefoundry/") || strings.HasPrefix(path, "output/")
}

func statusFromToken(token string) string {
	switch {
	case token == "??":
		return "A"
	case strings.Contains(token, "D"):
		return "D"
	case strings.Contains(token, "A"):
		return "A"
	default:
		return "M"
	}
}

func (r *Repository) SaveWorkspace(name, message string) ([]string, error) {
	if name == "" {
		return nil, errors.New("workspace name required")
	}
	path := r.WorkspacePath(name)
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("workspace %q not found", name)
	}

	changed, err := r.ChangedFiles(path)
	if err != nil {
		return nil, err
	}
	if len(changed) == 0 {
		return nil, errors.New("no changes to save")
	}
	if err := RewriteCanonicalFiles(path, changed); err != nil {
		return nil, err
	}

	result, err := ValidateRepository(path)
	if err != nil {
		return nil, err
	}
	if !result.OK() {
		return nil, fmt.Errorf("validation failed: %s", result.Issues[0].String())
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, err := r.runGit(path, "add", "-A"); err != nil {
		return nil, err
	}
	if message == "" {
		message = "Save workspace changes"
	}
	if _, err := r.runGit(path, "-c", "user.name=worktreefoundry", "-c", "user.email=worktreefoundry@local", "commit", "-m", message); err != nil {
		if strings.Contains(err.Error(), "nothing to commit") {
			return changed, nil
		}
		return nil, err
	}
	return changed, nil
}

func (r *Repository) runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func (r *Repository) RunGit(dir string, args ...string) (string, error) {
	return r.runGit(dir, args...)
}

func (r *Repository) RestoreObject(workspace, typeName, id string) error {
	if workspace == "" || workspace == "main" {
		return errors.New("cannot restore in main workspace")
	}
	path := r.WorkspacePath(workspace)
	rel := filepath.ToSlash(filepath.Join("data", typeName, id+".yaml"))
	if _, err := r.runGit(path, "checkout", "--", rel); err == nil {
		return nil
	}
	if _, err := r.runGit(path, "checkout", "main", "--", rel); err != nil {
		return err
	}
	return nil
}
