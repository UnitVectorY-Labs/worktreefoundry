# Config

This document describes the configuration files under `config/` that drive repository behavior.

## `config/schemas/<type>.schema.json`

One schema file per object type. These can be edited through the web UI in the Configuration section.

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

New type schemas can be created through the web UI. The UI validates that the schema conforms to the supported subset before saving.

## `config/constraints.json`

Optional repository-level constraints file. This can be edited through the web UI in the Configuration section.

Supported keys:

- `unique`: list of uniqueness constraints
  - item shape: `{ "type": "service", "field": "name" }`
  - Fields with unique constraints show a badge indicator in the object edit form.
- `foreignKeys`: list of foreign key constraints
  - item shape: `{ "fromType": "service", "fromField": "teamId", "toType": "team", "toField": "_id" }`
  - Optional `toDisplayField` overrides which field to display as the option label.

Foreign keys are validated against currently loaded object values. When editing objects, foreign key fields display the referenced item's display field value (from the UI config) instead of the raw ID.

## `config/ui.json`

Optional UI configuration file for display settings.

- `repoName`: display name for the repository shown in the top bar.
- `types`: per-type display configuration.
  - `displayField`: the field shown as the primary label for items. Must be a required field. Defaults to `_id`.
  - `fields`: ordered list of additional fields to show in the list view.

## Strictness

`worktreefoundry` validates config layout strictly:

- Allowed paths under `config/`:
  - `config/schemas/*.schema.json`
  - `config/constraints.json`
  - `config/ui.json`
- Other files/directories under `config/` are reported as layout validation issues.
