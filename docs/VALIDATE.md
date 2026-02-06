# Validate

`worktreefoundry validate` checks repository correctness.

## Command

```bash
worktreefoundry validate --repository /path/to/repo
```

Environment variable:

- `WORKTREEFOUNDRY_REPOSITORY`

## Validation stages

1. Layout validation
- `data/` and `config/` directory expectations.
- File naming and allowed-path checks.

2. Parse and invariant validation
- YAML parsing for each object file.
- `_id` and `_type` presence.
- `_id` filename match and UUID format.
- `_type` folder-name match.
- v1 shape limits (no nested objects, no arrays of objects).

3. Schema validation
- Per-type checks using `config/schemas/<type>.schema.json`.
- Required fields, type checks, enum/length/range checks.
- Schema intentionally excludes `_id` and `_type`.

4. Constraint validation
- `unique` constraints.
- `foreignKeys` constraints.

## Output

- Exit success with `validation passed` when no issues are found.
- On failure, emits issue lines with stage/path/field context and returns non-zero.

The exact same validator is used by CLI, web save, web merge, and export pre-check.
