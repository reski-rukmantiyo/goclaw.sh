# AGENTS.md

Guidance for CLAUDE when creating or updating SRS documentation in `docs/srs`.

## SRS Structure Requirements

- For every new SRS document, use the full template below.
- For every updated SRS document, add or normalize missing template sections unless the user explicitly asks for a narrower edit.
- Keep existing title, metadata, summary, scope, functional requirements, and design sections when they are already present, but normalize their shape to the template when the edit scope allows it.
- Use concrete project-specific wording instead of leaving placeholders in committed SRS files.
- Keep acceptance criteria as checklist items when documenting requirements that are not yet implemented.
- Use proposed error codes that match existing canonical error patterns where possible.

## Required SRS Template

````markdown
# Software Requirements Specification: [Feature Name]

**Project**: deka-virtual-machine
**Release**: [e.g. 2026.2.0]
**Version**: 0.1-draft
**Date**: [YYYY-MM-DD]
**Status**: Draft
**Difficulty**: [Low / Medium / High]
**Estimate**: [N days]

---

## Revision History

| Version | Date | Changes |
|---------|------|---------|
| 0.1-draft | [YYYY-MM-DD] | Initial draft. |

---

## 1. Summary

[One or two sentences describing what this SRS defines. Name the feature, its purpose, and the conditions it handles. If this document is scoped out of a larger SRS, say what it owns and what is specified elsewhere.]

## 2. Scope

**In scope**:

- [Capability or model item 1.]
- [Capability or model item 2.]
- [Capability or model item 3.]

**Out of scope**:

- [Excluded capability 1.]
- [Excluded capability 2.]
- [Excluded capability 3.]

## 3. Functional Requirements

### FR-00: [Operator or System Actor Behavior Title]

[One sentence describing what the operator or system can do or what the system must do.]

[Optional: a policy table or behavior summary if the requirement has multiple modes or values.]

| Value | Behavior | Does not apply to |
|-------|----------|-------------------|
| `value_a` | [What happens.] | [What it ignores.] |
| `value_b` | [What happens.] | [What it ignores.] |

Acceptance criteria:

- [ ] [Criterion 1.]
- [ ] [Criterion 2.]
- [ ] [Criterion 3.]

---

### FR-01: [Data Model or Fields Title]

[One sentence describing what must be persisted or exposed.]

Acceptance criteria:

- [ ] [Criterion 1.]
- [ ] [Criterion 2.]
- [ ] [Criterion 3.]

Persisted fields:

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `field_name` | type | default | [Notes.] |
| `field_name` | type | default | [Notes.] |

---

### FR-02: [API Title]

[One sentence describing what the API must expose.]

[Optional: include example request/response JSON blocks.]

VM create request fields:

```json
{
  "field_name": "value"
}
```

VM list/detail response fields:

```json
{
  "field_name": "value",
  "field_name_2": 0
}
```

Dedicated endpoint:

```text
PUT /api/v1/[resource]/{id}/[sub-resource]
```

Request body:

```json
{
  "field_name": "value"
}
```

Acceptance criteria:

- [ ] [Create/update accepts optional fields; omitted values use documented defaults.]
- [ ] [List and detail responses include relevant fields.]
- [ ] [Unknown resource IDs return canonical `resource.not_found`.]
- [ ] [Invalid field values return canonical `request.validation_failed`.]
- [ ] [Policy or config changes are audit logged with old value, new value, resource ID, request ID, and actor or system context.]
- [ ] [The endpoint does not expose secrets or raw internal errors.]

---

### FR-03: [Execution or Enforcement Title]

[One sentence describing the core execution or enforcement behavior.]

Acceptance criteria:

- [ ] [Criterion 1.]
- [ ] [Criterion 2.]
- [ ] [Criterion 3.]

[Optional: include formulas, sequences, or decision logic.]

```text
[formula or pseudocode]
```

---

### FR-04: [Task or Event Behavior Title]

[One sentence describing task or event requirements.]

Task requirements:

| Field | Requirement |
|-------|-------------|
| `type` | [Task type string.] |
| `resource_type` | [Resource string.] |
| `resource_id` | [Resource UUID.] |
| `resource_label` | [Resource name when available.] |
| `request_id` | [Generated system request ID when no user request caused the operation.] |
| `input` | [What to include.] |
| `result` | [What to include on success.] |
| `error` | [Error envelope requirement.] |

Acceptance criteria:

- [ ] [Criterion 1.]
- [ ] [Criterion 2.]
- [ ] [Criterion 3.]

---

### FR-05: [Cleanup or Readiness Title]

[One sentence describing cleanup or preflight readiness requirements.]

Acceptance criteria:

- [ ] [Criterion 1.]
- [ ] [Criterion 2.]
- [ ] [Criterion 3.]

---

### FR-06: [Operator Visibility Title]

[One sentence describing what operators must be able to see or manage.]

Acceptance criteria:

- [ ] [Create flow exposes [control/choice].]
- [ ] [Detail view displays [field/state].]
- [ ] [Edit flow exposes [control] without requiring unrelated changes.]
- [ ] [List or control bar displays compact [status].]
- [ ] [Audit logs record [event type].]
- [ ] [Structured logs include `resource_id`, `node_id`, and `request_id` or system actor context.]

## 4. System Impact

- [DB model change or migration.]
- [Service layer change.]
- [API handler or router change.]
- [Task or worker integration change.]
- [Vue UI change.]
- [OpenAPI schema change.]
- [Audit or structured logging change.]
- [Canonical error code registration.]

## 5. Test Plan

- Unit tests for [validation, formula, or logic].
- Service tests for [scenario A], [scenario B], [scenario C].
- Task tests for [success, failure, exhaustion payloads].
- Handler tests for [endpoint validation].
- UI tests for [create/detail/edit controls and error display].
- Manual integration test for [runtime or infrastructure scenario].

## 6. Risks and Open Questions

| Risk or question | Draft decision |
|------------------|----------------|
| [Risk or question 1.] | [Decision or TBD.] |
| [Risk or question 2.] | [Decision or TBD.] |
| [Risk or question 3.] | [Decision or TBD.] |

## 7. Implementation Plan

1. [Step 1: models, migrations, schema validation.]
2. [Step 2: API endpoint and service validation.]
3. [Step 3: core execution or enforcement logic.]
4. [Step 4: task integration and error handling.]
5. [Step 5: cleanup or readiness validation.]
6. [Step 6: UI controls and operator visibility.]
7. [Step 7: test coverage and manual validation.]

## 8. Proposed Error Codes

| Code | Meaning |
|------|---------|
| `resource.action_invalid` | [What the error means.] |
| `resource.action_failed` | [What the error means.] |
| `resource.not_found` | [Existing or new canonical not-found code.] |
````
