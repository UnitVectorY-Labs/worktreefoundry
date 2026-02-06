# Export

`worktreefoundry export` compiles repository objects into deterministic JSON artifacts.

## Command

```bash
worktreefoundry export --repository /path/to/repo --out /path/to/repo/output
```

`--out` can be absolute or relative to repository root. Default is `output`.

Environment variables:

- `WORKTREEFOUNDRY_REPOSITORY`
- `WORKTREEFOUNDRY_OUT`

## Behavior

- Runs full repository validation first.
- For each schema type, reads `data/<type>/*.yaml` objects.
- Writes `output/<type>.json` as an array.
- Strips `_id` and `_type` from exported objects.
- Sorts objects deterministically by `_id`.

## Determinism

The output order is stable for the same repository state.

## Failure model

Export fails when validation fails, so output artifacts only represent valid repository states.
