[![License](https://img.shields.io/badge/license-MIT-blue.svg)](https://opensource.org/licenses/MIT) [![Work In Progress](https://img.shields.io/badge/Status-Work%20In%20Progress-yellow)](https://guide.unitvectorylabs.com/bestpractices/status/#work-in-progress) [![Go Report Card](https://goreportcard.com/badge/github.com/UnitVectorY-Labs/worktreefoundry)](https://goreportcard.com/report/github.com/UnitVectorY-Labs/worktreefoundry)

# worktreefoundry

A schema-driven tool for managing configuration data stored in a Git repository.

`worktreefoundry` provides:
- A local web UI (`worktreefoundry web`) for browsing and editing objects in workspace branches.
- Repository validation (`worktreefoundry validate`) for layout, schema, and cross-object constraints.
- Deterministic export (`worktreefoundry export`) that compiles YAML objects into JSON artifacts.
- Repository bootstrap (`worktreefoundry init`) that initializes a sample repo with schemas and data.

See `/docs/README.md` for detailed documentation.
