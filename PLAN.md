Business Requirements Document

1) Overview

Build a schema driven configuration management platform where a local Git repository is the source of truth. Configuration objects are stored as YAML files. The application provides a generated UI from JSON Schema, enforces validation and cross object constraints, and publishes changes by merging a workspace branch into a protected main.

2) Goals
 - Provide a standalone server that can manage a local Git repo.
 - Eliminate the need for a custom web app per configuration domain by generating the UI from schemas.
 - Provide stable diffs and safer merges by operating on structured objects rather than raw YAML text.
 - Enforce repository wide correctness via validation, including foreign key style references and uniqueness constraints.
 - Provide an export step that compiles YAML objects into consumer friendly JSON artifacts.

3) Out of scope for initial version (v1)
 - User authentication and authorization. May be added later, but not a core bit of functionality.
 - Multi server or stateless deployment. First version assumes server has access to git repo on local disk and uses that to manage in progress state.
 - GitHub pull request creation, checks, and merge via API. Future enhancements will add support for pushing and managing github repository, but that is initially out of scope.
 - Nested objects, complex JSON types, and arrays of objects. Simplify the data model to speed up initial implementation and testing.

4) Users and primary use cases

Intended for a small number of users managing slowly changing configurations. When running locally only a single user will use the tool at a time.

Core functionality:
1.	Browse types and objects.
2.	Create, edit, and delete objects in a workspace branch. Create new branches for multiple workspaces.
3.	See “dirty” state for uncommitted edits.
4.	Validate changes in real time in the UI (done automatically as changes are made).
5.	Save changes (commit to workspace branch).
6.	Merge a workspace branch into main through an application controlled merge flow.
7.	Resolve merge conflicts at the field level with guided validation.
8.	Run repository validation from the command line.
9.	Export compiled JSON outputs from the command line.

5) Business requirements

BR-1: Standalone local mode
 - The product must run as a standalone server pointed at a local Git repo.
 - No external dependnecies are required, just the executable for worktreefoundry that has all dependencies including webpage templates built in.

BR-2: Repository as database
 - All authoritative data lives in the Git repo under strict layout and validation rules.
 - No external database is used.
 - Ideally updates made to files outside of the web app can be detected and validated by the CLI mode, but this can be deferred to future versions if documented as a limitation.

BR-3: Schema driven UI
 - UI is generated from JSON Schema to avoid per domain bespoke UI work.
 - UI validates in real time and prevents invalid proposals.
 - The subset of JSON Schema functionality is documented and validated against.
 - v1 does not allow defining the JSON Schema in the UI. Schemas must be pre defined in config/schemas/.

BR-4: Safe publication
 - Users do not edit main directly. The main branch is effectively read only and can only be updated via the merge flow after changes are made on a branch that lives in a worktree.
 - Publication is the act of merging a workspace branch into main via the app.
 - The app runs merge validation and refuses to publish if validation fails.
 - After a successful merge, the workspace branch is deleted.

BR-5: Governance friendly outputs
 - Repo format is opinionated for safety, stable diffs, and reviewability.
 - A separate export step produces consumer friendly artifacts for downstream systems.

6) Success criteria
 - A new repo can be initialized with data/ and config/ and becomes editable in the UI immediately. This includes initializing a git repository and the schema files.
 - Any edit that violates schema, uniqueness, or foreign key constraints is blocked at save/merge time and ideally earlier in the UI. If a branch is in an invalid state that is clearly visible to the user as a red flag in the main UI at the top of the page.
 - Merges do not require users to manually resolve YAML conflict markers.
 - CLI validation catches the same failures as server side validation.
 - Export produces deterministic JSON outputs that reflect the repository state.

⸻

Technical Requirements Document

1) System context

A single stateful Go server manages:
 - A local Git repository checkout
 - Workspace branches represented as Git worktrees
 - A web UI for object browsing, editing, validation, and merging
 - A CLI mode for validation and export
 - Minimal external dependencies. HTMX used for interactive web UI. Go standard library used for templates.

2) Repository layout and rules

(These are the requirements for the repository being used by worktreefoundry, not the worktreefoundry application code repo itself.)

2.1 Directories
 - data/ contains all configuration objects.
 - Objects are stored by type folder: data/<type>/<uuid>.yaml
 - config/ contains schemas and metadata.
 - JSON Schemas live under: config/schemas/<type>.schema.json
 - Additional config files for constraints and UI metadata may live under config/ (defined below).
 - output/ is used by export and must be gitignored.
 - All other paths in the repo are ignored by the app, except they must not violate the “strict allowed files” rule for data/ and config/.

2.2 Allowed files and strictness
 - Under data/:
	- Only directories (types) and .yaml files are allowed.
	- Filenames must be UUIDs with .yaml extension.
	- Comments are not allowed and are stripped on rewrite.
 - Under config/:
	- Allowed file set is strictly validated. At minimum:
	- config/schemas/*.schema.json
	- Additional metadata files as defined by the application (examples below).

2.3 Object file invariants
Each data/<type>/<id>.yaml file:
	- Must be valid YAML containing exactly one root mapping. (Using JSON Schema)
	- Must contain _id (required) and _type (required).
	- _id must match the filename UUID.
	- _type must match the parent folder name <type>.
	- Keys are canonicalized to alphabetical order at the top level (v1). No nested objects in v1.
	- Arrays preserve order.
 - Only allowed value shapes in v1:
	- Scalars: string, number, boolean (optional if you want it, otherwise restrict to string and number)
	- Arrays of strings or arrays of numbers only
	- No nested objects, no arrays of objects
	- Document this as a limitation clearly and understand future iterations would aim to ease these limitations.

3) Schema, constraints, and validation

3.1 JSON Schema usage (per type)
 - Each type has one schema: config/schemas/<type>.schema.json
 - The schema defines:
	- Required fields
	- Field types
	- Min and max for numbers
	- Min and max length for strings
	- Enums
 - The UI is generated from the schema:
	- Field widgets derived from type and enum
	- Required field indicators
	- Inline validation errors

3.2 Repository level constraints
In addition to JSON Schema, repo validation enforces:
 - Uniqueness constraints
	- A configured field must be unique across all objects of a type.
 - Foreign key constraints
	- A configured field must reference a field in another type.
	- In v1, simplest form: reference by _id of the target object type. Selection done by drop down.
	- Optionally allow referencing another unique field, but that requires lookup and strict uniqueness.

Store these constraints in a metadata file, for example:
 - config/constraints.json (or YAML)
	- unique: list of { type, field }
	- foreignKeys: list of { fromType, fromField, toType, toField }

UI behavior:
 - For foreign keys, show a picker listing eligible target objects.
 - Validate selection exists and remains valid.
 - For uniqueness, surface conflicts early during editing when feasible.

3.3 Validation entry points
Validation must be available in two modes and produce consistent results:
 - Server side validation used for:
 - Edit time checks
 - Save time checks
 - Merge time checks
 - CLI validation used for:
 - CI or pre merge style checks in future GitHub mode
 - Standalone repo linting

CLI command examples:
 - app validate --repo /path/to/repo
 - app export --repo /path/to/repo --out /path/to/repo/output

Validation stages:
	1.	Layout validation (data/ and config/ only allowed files)
	2.	Parse validation (YAML parse and invariant checks)
	3.	Schema validation (per type)
	4.	Constraint validation (uniqueness, foreign keys)
	5.	Optional: type folder and schema presence checks

4) Editing, workspaces, and “dirty” state

4.1 Branch model
 - main is protected by application policy:
	- The UI and server must never modify main directly.
	- The UI allows for selecting which branch to view, when main is selected it is read only.
 - Each user session creates a workspace branch:
	- workspace/{branch}
	- There is a one-to-one mapping between a workspace branch and a worktree on disk.
	- The names of branches that can be created are restricted using a regex to avoid problematic names.
 - Each workspace branch is represented as a Git worktree on disk.

4.2 Dirty state
 - Objects can be edited without committing.
 - The UI must show:
	- Branch dirty status
	- Which objects have uncommitted changes
 - Internally, dirty state can be computed by Git status of the worktree plus in memory staged edits.

4.3 Save flow (commit)
When the user chooses Save:
1.	The app computes the set of changed objects and shows a structured summary.
2.	The app rewrites YAML files for changed objects in canonical form.
3.	The app validates the workspace repo state.
4.	If valid, the app commits changes to the workspace branch.
5.	Local push is optional in local mode, but the branch must exist in the local repo.

The app may force push workspace branches in future remote mode, but in v1 it is purely local.

5) Structured diff model

5.1 Diff representation
 - The UI shows diffs per object as field level changes:
	- Added fields
	- Removed fields
	- Modified scalar values
	- Modified arrays as whole value with element level view optional

5.2 Diff source
 - Do not rely on raw text diff.
 - Parse YAML into typed structures and compare normalized representations.
 - Visually show the diffs with the values for the object for main on the left hand side and the workspace version on the right hand side. Make it easy to understand what values are being selected to resolve conflicts.
 - A branch can be deleted without merging if changes are to be abandoned.

6) Merge and conflict resolution

6.1 Publish event
 - Publish is the act of merging a committed workspace branch into main via the application.
 - After a successful merge:
	- main is updated
	- Workspace branch is deleted
	- Future versions may instead have merging occur on the remote so structure the code such that this is flexible, but the initial version is running in a local only mode.

6.2 Merge flow
1.	User selects a workspace branch to publish.
2.	App determines changed objects relative to main using Git to list changed files.
3.	For each changed object:
	- Load base version (merge base), main version, and workspace version.
	- Perform a three way structured comparison.

6.3 Conflict detection
 - A conflict exists when the same field was changed differently in main and workspace relative to base.
 - Conflicts are detected at the field level, not by YAML markers.

6.4 Conflict resolution UI
 - For each conflicting field, user chooses:
	- Take main
	- Take workspace
	- Manual entry (validated by schema)
 - Resolution produces a final object that:
	- Satisfies per type schema
	- Preserves required _id and _type
	- Satisfies repo constraints (uniqueness and foreign keys)
	- Is rewritten to canonical YAML

6.5 Merge validation and execution
 - App validates the full repo state of the would be merged result.
 - JSON Schema will not include the _id and _type fields during validation since they are invariant, the must be manually validated but the act of validating the JSON Schema will first remove those fields from the object being validated.
 - If validation passes:
	- Perform the merge into main in a controlled way.
	- Write canonical YAML for any objects touched by resolution.
	- Commit merge result to main.
 - If validation fails:
	- Merge is blocked with actionable errors.

7) Export

7.1 Export output
 - CLI command compiles YAML into JSON artifacts under output/ (gitignored).
 - For each type:
	- Read all objects of that type.
	- Validate first.
	- Output output/<type>.json as an array of objects.
	- Objects do not include _id and _type fields in output.
 - Output must be deterministic:
	- Stable ordering of objects in the array, for example sort by _id or configurable sort key.

8) Server architecture (Go)

8.1 Components
 - Repo manager
	- Opens and locks the repo
	- Creates and removes worktrees
	- Runs Git commands and abstracts errors
 - Object store
	- Loads objects from data/
	- Writes canonical YAML
	- Tracks type folders
 - Schema manager
	- Loads schemas from config/schemas/
	- Provides UI form metadata
	- Performs schema validation
 - Constraint engine
	- Loads config/constraints.*
	- Enforces uniqueness and foreign keys
 - Merge engine
	- Three way field comparator
	- Conflict model and resolution applier
 - Web API and UI
	- API endpoints for list, get, create, update, delete, diff, save, merge
	- Generated forms and validation feedback
	- The UI is server rendered using Go templates and HTMX for interactivity.
	- All static content must be embedded in the binary so it can function as a standalone executable.

8.2 Statefulness and concurrency
 - Single server instance is stateful and holds checked out repo state on disk.
 - Worktrees provide isolation per workspace branch.
 - Use a repo lock to avoid simultaneous destructive Git operations.
 - Concurrency is low, so correctness and simplicity are prioritized over scaling.

9) API requirements (high level)

Minimum API surface (illustrative, a REST API is not desired, this is an HTMX server rendered app):
 - GET /types
 - GET /types/{type}/objects
 - GET /types/{type}/objects/{id}
 - POST /types/{type}/objects (generates UUID, creates file)
 - PUT /types/{type}/objects/{id} (updates in workspace)
 - DELETE /types/{type}/objects/{id}
 - GET /workspaces
 - POST /workspaces (create workspace)
 - GET /workspaces/{ws}/status (dirty state, changed objects)
 - POST /workspaces/{ws}/save (validate and commit)
 - POST /workspaces/{ws}/merge (merge into main with conflict model)
 - POST /validate (server side full validation)
 - POST /export (optional server triggered export, CLI is primary)

10) Acceptance criteria
 - The server can be pointed at a local repo and will:
	- Enforce directory layout rules
	- Load schemas
	- List types and objects
	- Create an object with generated UUID
	- Edit an object and show real time validation
	- Save changes into a workspace commit
	- Merge workspace into main only when full validation passes
	- Delete workspace branch on successful merge
 - Merge conflicts are presented as per field conflicts and can be resolved without editing YAML.
 - Running app validate produces pass or fail consistent with server behavior.
 - Running app export generates deterministic output/<type>.json files.
 - Documentation for this projects exists in docs/ including multiple markdown files describing high level functionality, configuration, and web UI. These must all be updated with the details of the functionality that has been implemented.

11) Risks and mitigations
 - Schema driven UI complexity
	- Mitigation: constrain v1 types to flat objects and simple arrays only.
 - Canonical YAML writing
	- Mitigation: always rewrite objects from parsed structure, strip comments, enforce key ordering.
 - Git edge cases
	- Mitigation: limit supported operations, use worktrees, enforce server level repo lock, provide clear error messages.
 - Constraint performance
	- Mitigation: brute force scans are acceptable for small and slowly changing repos in v1.

12) Future extensions

Do not work on implementing any of the following functionality but be aware these features may be desired in future versions and therefore the code should be structured to allow for their addition later while not implementing them now.

 - GitHub mode:
	- Workspace branches push to remote
	- PR creation and status checks
	- Validation as a standalone command in CI
 - Authentication and role based permissions
 - Nested objects and arrays of objects
 - Richer reference types and referential actions
 - Incremental export and indexes
 - Multi instance stateless server with shared storage and locking

 ---

 Requirements Caveat... The above requirements are presented as a best effort in attempting to describe the desired functionality.  It is possible there are better ways of implementing the functionality or internal inconsistences or errors in the requirements.  It is your job to always use your best judgement in interpreting these requirements and asking for clarifications when needed.