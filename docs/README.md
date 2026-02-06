# worktreefoundry

`worktreefoundry` is a standalone command line and web application for schema-driven configuration repositories.

## Implemented commands

- `worktreefoundry init --repository /path/to/repo`
  - Initializes a local Git repository on `main`.
  - Creates `data/`, `config/`, sample schemas, sample constraints, and sample objects.
  - Creates `.gitignore` entry for `output/`.

- `worktreefoundry validate --repository /path/to/repo`
  - Runs repository validation stages shared with the web application.

- `worktreefoundry export --repository /path/to/repo [--out output]`
  - Validates first, then compiles deterministic JSON outputs under `output/` (or custom `--out`).

- `worktreefoundry web --repository /path/to/repo [--addr :8080]`
  - Hosts a local server for browsing, editing, saving, validating, and merging workspace branches.

## Environment variables

All command flags have env-var counterparts:

- `WORKTREEFOUNDRY_REPOSITORY`
- `WORKTREEFOUNDRY_WORKSPACE_ROOT`
- `WORKTREEFOUNDRY_ADDR`
- `WORKTREEFOUNDRY_OUT`

## Repository model

- Data objects are stored at `data/<type>/<uuid>.yaml`.
- Schemas are loaded from `config/schemas/<type>.schema.json`.
- Cross-object constraints are loaded from `config/constraints.json`.
- `main` is read-only in the UI.
- Users edit inside workspace branches (`workspace/<name>`) backed by Git worktrees.

## Web flow

- Create/select workspace.
- Edit object files using schema-generated form fields.
- Review dirty files.
- Save (commit) workspace changes.
- Merge workspace into `main` with field-level conflict handling.

## Validation consistency

The same validation engine is used by:

- `validate` CLI command
- Save-time checks in the web flow
- Merge-time checks in the web flow
- Export pre-check
