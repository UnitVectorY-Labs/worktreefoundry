# Config

This document describes the configuration files under `config/` that drive repository behavior.

## `config/schemas/<type>.schema.json`

One schema file per object type.

Supported JSON Schema subset in v1:

- Root `type` must be `object`.
- `required` list for required fields.
- `properties` for field definitions.
- Field `type` supports:
  - `string`
  - `number`
  - `integer`
  - `boolean`
  - `array` (items must be `string`, `number`, or `integer`)
- Supported field constraints:
  - `minLength`, `maxLength` for strings
  - `minimum`, `maximum` for numbers/integers
  - `enum` for strings

Not supported in v1:

- Nested objects
- Arrays of objects
- `_id` and `_type` definitions inside schema properties (these are repository invariants, not schema fields)

## `config/constraints.json`

Optional repository-level constraints file.

Supported keys:

- `unique`: list of uniqueness constraints
  - item shape: `{ "type": "service", "field": "name" }`
- `foreignKeys`: list of foreign key constraints
  - item shape: `{ "fromType": "service", "fromField": "teamId", "toType": "team", "toField": "_id" }`

Foreign keys are validated against currently loaded object values.

## Strictness

`worktreefoundry` validates config layout strictly:

- Allowed paths under `config/`:
  - `config/schemas/*.schema.json`
  - `config/constraints.json`
- Other files/directories under `config/` are reported as layout validation issues.
