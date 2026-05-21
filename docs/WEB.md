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

### Branding

The top bar shows "Worktree Foundry" as the application name, with the repository name displayed below it. The header uses color-coded accent bars to indicate the current mode:

- **Blue accent** when viewing the main branch (read-only)
- **Green accent** when working in a workspace branch (editable)
- **Purple accent** when in the configuration section

### Workspace model

- `main` is read-only.
- Editable changes happen in workspace branches (`workspace/<name>`) with dedicated Git worktrees.
- Workspace view shows dirty status and changed files.
- The "New Workspace" button is only visible when on the main branch. Workspaces branch from main and merge back into main.

### Object editing

- Types and fields are generated from `config/schemas/*.schema.json`.
- Form widgets are selected from field type (`string`, `number`, `integer`, `boolean`, `array`, enums).
- Objects are written to `data/<type>/<uuid>.yaml`.
- YAML is canonicalized on write.
- Fields marked with unique constraints show a "unique" badge indicator.
- Foreign key fields display the referenced item's display field value instead of the raw ID.
- Breadcrumbs use the configured display field value instead of the UUID when navigating to an individual item.

### Save flow

- Save navigates to a confirmation page showing all changed files.
- Save commits current workspace changes.
- Canonical rewrite is applied before commit.
- Validation is run before commit is allowed.

### Merge flow

- Merge navigates to a confirmation page showing the data files that differ between the workspace and main.
- Merge compares workspace branch against `main` using structured object fields.
- Field-level conflicts are shown with:
  - take `main`
  - take `workspace`
  - manual value
- Merge only commits when full repository validation passes.
- On successful merge, workspace branch/worktree are deleted.

### Validation from UI

The Validate action runs the same repository validation engine as CLI. When run from a workspace branch, it additionally performs a merge preview—simulating the merge of workspace changes onto main and validating the combined result.

### Configuration

The Config page provides access to:

- **Repository Configuration**: Edit the repository display name.
- **Type Display Configuration**: Configure which field is used as the display field and which additional fields appear in the list view for each type.
- **JSON Schemas**: View and edit the JSON Schema definition for each type. New types can be created from this section. Schema changes are validated against the supported subset before saving.
- **Constraints**: Edit unique and foreign key constraint definitions as JSON.
