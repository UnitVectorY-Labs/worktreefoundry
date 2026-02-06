# Web

`worktreefoundry web` hosts a local server UI for schema-driven object management.

## Start

```bash
worktreefoundry web --repository /path/to/repo --addr :8080
```

Environment variable equivalents:

- `WORKTREEFOUNDRY_REPOSITORY`
- `WORKTREEFOUNDRY_ADDR`
- `WORKTREEFOUNDRY_WORKSPACE_ROOT`

## UI behavior

The UI is server-rendered with Go templates and progressively enhanced with HTMX/static JS.

### Workspace model

- `main` is read-only.
- Editable changes happen in workspace branches (`workspace/<name>`) with dedicated Git worktrees.
- Workspace view shows dirty status and changed files.

### Object editing

- Types and fields are generated from `config/schemas/*.schema.json`.
- Form widgets are selected from field type (`string`, `number`, `integer`, `boolean`, `array`, enums).
- Objects are written to `data/<type>/<uuid>.yaml`.
- YAML is canonicalized on write.

### Save flow

- Save commits current workspace changes.
- Canonical rewrite is applied before commit.
- Validation is run before commit is allowed.

### Merge flow

- Merge compares workspace branch against `main` using structured object fields.
- Field-level conflicts are shown with:
  - take `main`
  - take `workspace`
  - manual value
- Merge only commits when full repository validation passes.
- On successful merge, workspace branch/worktree are deleted.

### Validation from UI

The Validate action runs the same repository validation engine as CLI.
