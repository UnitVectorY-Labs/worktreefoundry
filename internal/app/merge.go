package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

type FieldConflict struct {
	File      string
	Field     string
	Base      any
	Main      any
	Workspace any
	Key       string
}

type MergeResult struct {
	Merged      bool
	Changed     []string
	Conflicts   []FieldConflict
	Message     string
	Workspace   string
	MergedFiles int
}

func (r *Repository) MergeWorkspace(name string, resolutions map[string]string, manualValues map[string]string) (MergeResult, error) {
	path := r.WorkspacePath(name)
	if _, err := os.Stat(path); err != nil {
		return MergeResult{}, fmt.Errorf("workspace %q not found", name)
	}
	branch := r.BranchForWorkspace(name)

	r.mu.Lock()
	defer r.mu.Unlock()

	if branchName, err := r.CurrentBranch(r.Root); err != nil {
		return MergeResult{}, err
	} else if branchName != "main" {
		return MergeResult{}, fmt.Errorf("main worktree must be on main branch (current: %s)", branchName)
	}
	if changed, err := r.ChangedFiles(r.Root); err != nil {
		return MergeResult{}, err
	} else if len(changed) > 0 {
		return MergeResult{}, errors.New("main worktree has uncommitted changes")
	}

	changedFiles, err := r.diffWorkspaceDataFiles(branch)
	if err != nil {
		return MergeResult{}, err
	}
	if len(changedFiles) == 0 {
		return MergeResult{Merged: false, Workspace: name, Message: "no changes to merge"}, nil
	}

	mergedFiles := map[string]*map[string]any{}
	conflicts := make([]FieldConflict, 0)

	for _, rel := range changedFiles {
		baseMap, _ := r.readObjectAtRef("main", rel)
		mainMap, _ := r.readObjectAtRef("main", rel)
		wsMap, _ := r.readObjectAtRef(branch, rel)
		if baseSha, err := r.mergeBase("main", branch); err == nil {
			if m, ok := r.readObjectAtRef(baseSha, rel); ok {
				baseMap = m
			} else {
				baseMap = nil
			}
		}

		merged, fileConflicts := mergeThreeWayObject(rel, baseMap, mainMap, wsMap, resolutions, manualValues)
		if len(fileConflicts) > 0 {
			conflicts = append(conflicts, fileConflicts...)
			continue
		}
		mergedFiles[rel] = merged
	}

	if len(conflicts) > 0 {
		sort.Slice(conflicts, func(i, j int) bool {
			if conflicts[i].File == conflicts[j].File {
				return conflicts[i].Field < conflicts[j].Field
			}
			return conflicts[i].File < conflicts[j].File
		})
		return MergeResult{
			Merged:    false,
			Workspace: name,
			Changed:   changedFiles,
			Conflicts: conflicts,
			Message:   "conflicts require resolution",
		}, nil
	}

	backups, err := backupPaths(r.Root, changedFiles)
	if err != nil {
		return MergeResult{}, err
	}
	rollback := func() {
		_ = restorePaths(r.Root, backups)
	}

	for _, rel := range changedFiles {
		full := filepath.Join(r.Root, filepath.FromSlash(rel))
		merged := mergedFiles[rel]
		if merged == nil || len(*merged) == 0 {
			if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
				rollback()
				return MergeResult{}, err
			}
			continue
		}
		obj, err := objectFromPathAndData(rel, *merged)
		if err != nil {
			rollback()
			return MergeResult{}, err
		}
		if err := WriteObject(r.Root, obj); err != nil {
			rollback()
			return MergeResult{}, err
		}
	}

	if validation, err := ValidateRepository(r.Root); err != nil {
		rollback()
		return MergeResult{}, err
	} else if !validation.OK() {
		rollback()
		return MergeResult{}, fmt.Errorf("merge blocked by validation: %s", validation.Issues[0].String())
	}

	if _, err := r.runGit(r.Root, "add", "-A"); err != nil {
		rollback()
		return MergeResult{}, err
	}
	if _, err := r.runGit(r.Root, "-c", "user.name=worktreefoundry", "-c", "user.email=worktreefoundry@local", "commit", "-m", fmt.Sprintf("Merge %s into main", branch)); err != nil {
		rollback()
		return MergeResult{}, err
	}

	if err := r.deleteWorkspaceLocked(name); err != nil {
		return MergeResult{}, err
	}

	return MergeResult{Merged: true, Workspace: name, Changed: changedFiles, MergedFiles: len(changedFiles), Message: "merge complete"}, nil
}

func (r *Repository) diffWorkspaceDataFiles(branch string) ([]string, error) {
	return r.DiffWorkspaceDataFiles(branch)
}

func (r *Repository) DiffWorkspaceDataFiles(branch string) ([]string, error) {
	out, err := r.runGit(r.Root, "diff", "--name-only", "main.."+branch, "--", "data")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	files := make([]string, 0)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = filepath.ToSlash(line)
		if strings.HasPrefix(line, "data/") && strings.HasSuffix(line, ".yaml") {
			files = append(files, line)
		}
	}
	sort.Strings(files)
	return files, nil
}

func (r *Repository) mergeBase(a, b string) (string, error) {
	out, err := r.runGit(r.Root, "merge-base", a, b)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (r *Repository) readObjectAtRef(ref, relPath string) (map[string]any, bool) {
	out, err := r.runGit(r.Root, "show", fmt.Sprintf("%s:%s", ref, relPath))
	if err != nil {
		return nil, false
	}
	m, err := ParseSimpleYAMLObject([]byte(out))
	if err != nil {
		return nil, false
	}
	normalized := make(map[string]any, len(m))
	for k, v := range m {
		nv, err := normalizeObjectValue(v)
		if err != nil {
			return nil, false
		}
		normalized[k] = nv
	}
	return normalized, true
}

func mergeThreeWayObject(rel string, base, main, ws map[string]any, resolutions, manual map[string]string) (*map[string]any, []FieldConflict) {
	keys := map[string]struct{}{}
	for k := range base {
		keys[k] = struct{}{}
	}
	for k := range main {
		keys[k] = struct{}{}
	}
	for k := range ws {
		keys[k] = struct{}{}
	}

	keyList := make([]string, 0, len(keys))
	for k := range keys {
		keyList = append(keyList, k)
	}
	sort.Strings(keyList)

	merged := make(map[string]any)
	conflicts := make([]FieldConflict, 0)

	for _, field := range keyList {
		b, bOK := base[field]
		m, mOK := main[field]
		w, wOK := ws[field]

		if !bOK {
			b = nil
		}
		if !mOK {
			m = nil
		}
		if !wOK {
			w = nil
		}

		if reflect.DeepEqual(m, w) {
			if mOK {
				merged[field] = m
			}
			continue
		}
		if reflect.DeepEqual(m, b) {
			if wOK {
				merged[field] = w
			}
			continue
		}
		if reflect.DeepEqual(w, b) {
			if mOK {
				merged[field] = m
			}
			continue
		}

		key := conflictKey(rel, field)
		choice := resolutions[key]
		switch choice {
		case "main":
			if mOK {
				merged[field] = m
			}
		case "workspace":
			if wOK {
				merged[field] = w
			}
		case "manual":
			manualValue, err := parseManualFieldValue(manual[key])
			if err != nil {
				conflicts = append(conflicts, FieldConflict{File: rel, Field: field, Base: b, Main: m, Workspace: w, Key: key})
				continue
			}
			if manualValue != nil {
				merged[field] = manualValue
			}
		default:
			conflicts = append(conflicts, FieldConflict{File: rel, Field: field, Base: b, Main: m, Workspace: w, Key: key})
		}
	}

	if _, ok := merged["_id"]; !ok {
		if base != nil {
			return nil, conflicts
		}
	}
	if len(merged) == 0 {
		return nil, conflicts
	}
	return &merged, conflicts
}

func parseManualFieldValue(raw string) (any, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	if strings.Contains(trimmed, ",") {
		parts := strings.Split(trimmed, ",")
		arr := make([]any, 0, len(parts))
		for _, p := range parts {
			v, err := parseYAMLScalar(strings.TrimSpace(p))
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		return arr, nil
	}
	return parseYAMLScalar(trimmed)
}

func conflictKey(file, field string) string {
	return file + "::" + field
}

type fileBackup struct {
	rel    string
	exists bool
	data   []byte
}

func backupPaths(root string, relPaths []string) ([]fileBackup, error) {
	backups := make([]fileBackup, 0, len(relPaths))
	for _, rel := range relPaths {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		b := fileBackup{rel: rel}
		if data, err := os.ReadFile(abs); err == nil {
			b.exists = true
			b.data = data
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		backups = append(backups, b)
	}
	return backups, nil
}

func restorePaths(root string, backups []fileBackup) error {
	for _, b := range backups {
		abs := filepath.Join(root, filepath.FromSlash(b.rel))
		if b.exists {
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(abs, b.data, 0o644); err != nil {
				return err
			}
		} else {
			if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}

func objectFromPathAndData(rel string, data map[string]any) (Object, error) {
	parts := strings.Split(rel, "/")
	if len(parts) != 3 || parts[0] != "data" || !strings.HasSuffix(parts[2], ".yaml") {
		return Object{}, fmt.Errorf("invalid data path %q", rel)
	}
	typeName := parts[1]
	id := strings.TrimSuffix(parts[2], ".yaml")
	obj := Object{ID: id, Type: typeName, Data: data, Path: rel}
	if got, _ := data["_id"].(string); got != "" && got != id {
		return Object{}, fmt.Errorf("_id %q does not match path id %q", got, id)
	}
	if got, _ := data["_type"].(string); got != "" && got != typeName {
		return Object{}, fmt.Errorf("_type %q does not match path type %q", got, typeName)
	}
	if _, ok := data["_id"]; !ok {
		obj.Data["_id"] = id
	}
	if _, ok := data["_type"]; !ok {
		obj.Data["_type"] = typeName
	}
	return obj, nil
}

// ValidateMergePreview simulates merging the workspace into main and validates the merged result.
func (r *Repository) ValidateMergePreview(name string) (ValidationResult, error) {
	path := r.WorkspacePath(name)
	if _, err := os.Stat(path); err != nil {
		return ValidationResult{}, fmt.Errorf("workspace %q not found", name)
	}

	// First validate the workspace itself
	wsResult, err := ValidateRepository(path)
	if err != nil {
		return ValidationResult{}, err
	}
	if !wsResult.OK() {
		return wsResult, nil
	}

	// Check if main has uncommitted changes
	mainChanged, err := r.ChangedFiles(r.Root)
	if err != nil {
		return ValidationResult{}, err
	}
	if len(mainChanged) > 0 {
		result := ValidationResult{}
		result.Add(ValidationIssue{Stage: "merge-preview", Message: "main has uncommitted changes; cannot preview merge"})
		return result, nil
	}

	branch := r.BranchForWorkspace(name)
	changedFiles, err := r.DiffWorkspaceDataFiles(branch)
	if err != nil {
		return ValidationResult{}, err
	}
	if len(changedFiles) == 0 {
		// No data changes to merge — workspace validates clean
		return wsResult, nil
	}

	// Simulate the merge: apply workspace changes onto main in a temp directory
	tmpDir, err := os.MkdirTemp("", "worktreefoundry-merge-preview-*")
	if err != nil {
		return ValidationResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	// Copy main's config and data directories to temp
	if err := copyDir(filepath.Join(r.Root, "config"), filepath.Join(tmpDir, "config")); err != nil {
		return ValidationResult{}, err
	}
	if err := copyDir(filepath.Join(r.Root, "data"), filepath.Join(tmpDir, "data")); err != nil {
		return ValidationResult{}, err
	}

	// Apply workspace data files (config changes from workspace also)
	if err := copyDir(filepath.Join(path, "config"), filepath.Join(tmpDir, "config")); err != nil {
		return ValidationResult{}, err
	}

	// Apply changed data files from workspace onto the preview
	for _, rel := range changedFiles {
		srcPath := filepath.Join(path, filepath.FromSlash(rel))
		dstPath := filepath.Join(tmpDir, filepath.FromSlash(rel))
		if _, err := os.Stat(srcPath); err != nil {
			// File deleted in workspace
			_ = os.Remove(dstPath)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return ValidationResult{}, err
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return ValidationResult{}, err
		}
		if err := os.WriteFile(dstPath, data, 0o644); err != nil {
			return ValidationResult{}, err
		}
	}

	return ValidateRepository(tmpDir)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
