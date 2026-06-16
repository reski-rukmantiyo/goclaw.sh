//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
)

//go:embed schema.sql
var schemaSQL string

// SchemaVersion is the current SQLite schema version.
// Bump this when adding new migration steps below.
const SchemaVersion = 47

// migrations maps version → SQL to apply when upgrading FROM that version.
// schema.sql always represents the LATEST full schema (for fresh DBs).
// Existing DBs are patched incrementally via these steps.
//
// Example: to add a new column in the future:
//
//	var migrations = map[int]string{
//	    1: `ALTER TABLE agents ADD COLUMN new_col TEXT DEFAULT '';`,
//	}
//
// Then bump SchemaVersion to 2.
var migrations = map[int]string{
	// Version 1 → 2: add contact_type column to channel_contacts.
	1: `ALTER TABLE channel_contacts ADD COLUMN contact_type VARCHAR(20) NOT NULL DEFAULT 'user';`,
	// Version 2 → 3: promote cron payload fields to dedicated columns + add stateless flag.
	2: `ALTER TABLE cron_jobs ADD COLUMN stateless INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cron_jobs ADD COLUMN deliver INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cron_jobs ADD COLUMN deliver_channel TEXT NOT NULL DEFAULT '';
ALTER TABLE cron_jobs ADD COLUMN deliver_to TEXT NOT NULL DEFAULT '';
ALTER TABLE cron_jobs ADD COLUMN wake_heartbeat INTEGER NOT NULL DEFAULT 0;
UPDATE cron_jobs SET
  deliver = COALESCE(json_extract(payload, '$.deliver'), 0),
  deliver_channel = COALESCE(json_extract(payload, '$.channel'), ''),
  deliver_to = COALESCE(json_extract(payload, '$.to'), ''),
  wake_heartbeat = COALESCE(json_extract(payload, '$.wake_heartbeat'), 0)
WHERE payload IS NOT NULL;`,
	// Version 4 → 5: add thread_id, thread_type columns to channel_contacts for forum topic support.
	4: `ALTER TABLE channel_contacts ADD COLUMN thread_id VARCHAR(100);
ALTER TABLE channel_contacts ADD COLUMN thread_type VARCHAR(20);
DROP INDEX IF EXISTS idx_channel_contacts_tenant_type_sender;
CREATE UNIQUE INDEX idx_channel_contacts_tenant_type_sender
  ON channel_contacts(tenant_id, channel_type, sender_id, COALESCE(thread_id, ''));`,
	// Version 3 → 4: add subagent_tasks table for subagent lifecycle persistence.
	3: `CREATE TABLE IF NOT EXISTS subagent_tasks (
    id                TEXT PRIMARY KEY,
    tenant_id         TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    parent_agent_key  VARCHAR(255) NOT NULL,
    session_key       VARCHAR(500),
    subject           VARCHAR(255) NOT NULL,
    description       TEXT NOT NULL,
    status            VARCHAR(20) NOT NULL DEFAULT 'running',
    result            TEXT,
    depth             INTEGER NOT NULL DEFAULT 1,
    model             VARCHAR(255),
    provider          VARCHAR(255),
    iterations        INTEGER NOT NULL DEFAULT 0,
    input_tokens      INTEGER NOT NULL DEFAULT 0,
    output_tokens     INTEGER NOT NULL DEFAULT 0,
    origin_channel    VARCHAR(50),
    origin_chat_id    VARCHAR(255),
    origin_peer_kind  VARCHAR(20),
    origin_user_id    VARCHAR(255),
    spawned_by        TEXT,
    completed_at      TEXT,
    archived_at       TEXT,
    metadata          TEXT NOT NULL DEFAULT '{}',
    created_at        TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at        TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_subagent_tasks_parent_status ON subagent_tasks(tenant_id, parent_agent_key, status);
CREATE INDEX IF NOT EXISTS idx_subagent_tasks_session ON subagent_tasks(session_key);
CREATE INDEX IF NOT EXISTS idx_subagent_tasks_created ON subagent_tasks(tenant_id, created_at);`,
	// Version 5 → 6: secure CLI agent grants — replace agent_id with is_global + grants table.
	5: `ALTER TABLE secure_cli_binaries ADD COLUMN is_global BOOLEAN NOT NULL DEFAULT 1;
DROP INDEX IF EXISTS idx_secure_cli_unique_binary_agent;
DROP INDEX IF EXISTS idx_secure_cli_agent_id;
CREATE UNIQUE INDEX IF NOT EXISTS idx_secure_cli_unique_binary_tenant ON secure_cli_binaries(binary_name, tenant_id);
CREATE TABLE IF NOT EXISTS secure_cli_agent_grants (
    id              TEXT NOT NULL PRIMARY KEY,
    binary_id       TEXT NOT NULL REFERENCES secure_cli_binaries(id) ON DELETE CASCADE,
    agent_id        TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    deny_args       TEXT,
    deny_verbose    TEXT,
    timeout_seconds INTEGER,
    tips            TEXT,
    enabled         BOOLEAN NOT NULL DEFAULT 1,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id),
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(binary_id, agent_id, tenant_id)
);
CREATE INDEX IF NOT EXISTS idx_scag_binary ON secure_cli_agent_grants(binary_id);
CREATE INDEX IF NOT EXISTS idx_scag_agent ON secure_cli_agent_grants(agent_id);
CREATE INDEX IF NOT EXISTS idx_scag_tenant ON secure_cli_agent_grants(tenant_id);`,
	// Version 6 → 7: V3 tables (episodic, evolution, KG temporal) + promote other_config fields.
	6: `-- V3: episodic summaries
CREATE TABLE IF NOT EXISTS episodic_summaries (
    id          TEXT NOT NULL PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id     VARCHAR(255) NOT NULL DEFAULT '',
    session_key TEXT NOT NULL,
    summary     TEXT NOT NULL,
    l0_abstract TEXT NOT NULL DEFAULT '',
    key_topics  TEXT NOT NULL DEFAULT '[]',
    source_type TEXT NOT NULL DEFAULT 'session',
    source_id   TEXT,
    turn_count  INTEGER NOT NULL DEFAULT 0,
    token_count INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    expires_at  TEXT
);
CREATE INDEX IF NOT EXISTS idx_episodic_agent_user ON episodic_summaries(agent_id, user_id);
CREATE INDEX IF NOT EXISTS idx_episodic_tenant ON episodic_summaries(tenant_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_episodic_source_dedup ON episodic_summaries(agent_id, user_id, source_id)
    WHERE source_id IS NOT NULL;

-- V3: evolution metrics
CREATE TABLE IF NOT EXISTS agent_evolution_metrics (
    id          TEXT NOT NULL PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    session_key TEXT NOT NULL,
    metric_type TEXT NOT NULL,
    metric_key  TEXT NOT NULL,
    value       TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_evo_metrics_agent_type ON agent_evolution_metrics(agent_id, metric_type);
CREATE INDEX IF NOT EXISTS idx_evo_metrics_created ON agent_evolution_metrics(created_at);
CREATE INDEX IF NOT EXISTS idx_evo_metrics_tenant ON agent_evolution_metrics(tenant_id);

-- V3: evolution suggestions
CREATE TABLE IF NOT EXISTS agent_evolution_suggestions (
    id              TEXT NOT NULL PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id),
    agent_id        TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    suggestion_type TEXT NOT NULL,
    suggestion      TEXT NOT NULL,
    rationale       TEXT NOT NULL,
    parameters      TEXT,
    status          TEXT NOT NULL DEFAULT 'pending',
    reviewed_by     TEXT,
    reviewed_at     TEXT,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_evo_suggestions_agent ON agent_evolution_suggestions(agent_id, status);
CREATE INDEX IF NOT EXISTS idx_evo_suggestions_tenant ON agent_evolution_suggestions(tenant_id);

-- V3: KG temporal validity
ALTER TABLE kg_entities ADD COLUMN valid_from TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
ALTER TABLE kg_entities ADD COLUMN valid_until TEXT;
CREATE INDEX IF NOT EXISTS idx_kg_entities_current ON kg_entities(agent_id, user_id) WHERE valid_until IS NULL;

ALTER TABLE kg_relations ADD COLUMN valid_from TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
ALTER TABLE kg_relations ADD COLUMN valid_until TEXT;
CREATE INDEX IF NOT EXISTS idx_kg_relations_current ON kg_relations(agent_id, user_id) WHERE valid_until IS NULL;

-- Promote other_config fields to dedicated columns
ALTER TABLE agents ADD COLUMN emoji TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN agent_description TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN thinking_level TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN max_tokens INT NOT NULL DEFAULT 0;
ALTER TABLE agents ADD COLUMN self_evolve BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE agents ADD COLUMN skill_evolve BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE agents ADD COLUMN skill_nudge_interval INT NOT NULL DEFAULT 0;
ALTER TABLE agents ADD COLUMN reasoning_config TEXT NOT NULL DEFAULT '{}';
ALTER TABLE agents ADD COLUMN workspace_sharing TEXT NOT NULL DEFAULT '{}';
ALTER TABLE agents ADD COLUMN chatgpt_oauth_routing TEXT NOT NULL DEFAULT '{}';
ALTER TABLE agents ADD COLUMN shell_deny_groups TEXT NOT NULL DEFAULT '{}';
ALTER TABLE agents ADD COLUMN kg_dedup_config TEXT NOT NULL DEFAULT '{}';
UPDATE agents SET
  emoji = COALESCE(json_extract(other_config, '$.emoji'), ''),
  agent_description = COALESCE(json_extract(other_config, '$.description'), ''),
  thinking_level = COALESCE(json_extract(other_config, '$.thinking_level'), ''),
  max_tokens = COALESCE(json_extract(other_config, '$.max_tokens'), 0),
  self_evolve = COALESCE(json_extract(other_config, '$.self_evolve'), 0),
  skill_evolve = COALESCE(json_extract(other_config, '$.skill_evolve'), 0),
  skill_nudge_interval = COALESCE(json_extract(other_config, '$.skill_nudge_interval'), 0),
  reasoning_config = COALESCE(json_extract(other_config, '$.reasoning'), '{}'),
  workspace_sharing = COALESCE(json_extract(other_config, '$.workspace_sharing'), '{}'),
  chatgpt_oauth_routing = COALESCE(json_extract(other_config, '$.chatgpt_oauth_routing'), '{}'),
  shell_deny_groups = COALESCE(json_extract(other_config, '$.shell_deny_groups'), '{}'),
  kg_dedup_config = COALESCE(json_extract(other_config, '$.kg_dedup_config'), '{}')
WHERE other_config != '{}' AND other_config IS NOT NULL;
UPDATE agents SET other_config = json_remove(other_config,
  '$.emoji', '$.description', '$.thinking_level', '$.max_tokens',
  '$.self_evolve', '$.skill_evolve', '$.skill_nudge_interval',
  '$.reasoning', '$.workspace_sharing', '$.chatgpt_oauth_routing',
  '$.shell_deny_groups', '$.kg_dedup_config');`,

	// Version 7 → 8: add promoted_at to episodic_summaries for dreaming pipeline.
	7: `ALTER TABLE episodic_summaries ADD COLUMN promoted_at TEXT;
CREATE INDEX IF NOT EXISTS idx_episodic_unpromoted ON episodic_summaries(agent_id, user_id, created_at)
    WHERE promoted_at IS NULL;`,

	// Version 8 → 9: add kg_dedup_candidates, secure_cli_user_credentials, vault_documents, vault_links.
	8: `CREATE TABLE IF NOT EXISTS kg_dedup_candidates (
    id          TEXT NOT NULL PRIMARY KEY,
    tenant_id   TEXT REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id     VARCHAR(255) NOT NULL DEFAULT '',
    entity_a_id TEXT NOT NULL REFERENCES kg_entities(id) ON DELETE CASCADE,
    entity_b_id TEXT NOT NULL REFERENCES kg_entities(id) ON DELETE CASCADE,
    similarity  REAL NOT NULL,
    status      VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(entity_a_id, entity_b_id)
);
CREATE INDEX IF NOT EXISTS idx_kg_dedup_agent ON kg_dedup_candidates(agent_id, status);

CREATE TABLE IF NOT EXISTS secure_cli_user_credentials (
    id            TEXT NOT NULL PRIMARY KEY,
    binary_id     TEXT NOT NULL REFERENCES secure_cli_binaries(id) ON DELETE CASCADE,
    user_id       VARCHAR(255) NOT NULL,
    encrypted_env BLOB NOT NULL,
    metadata      TEXT NOT NULL DEFAULT '{}',
    tenant_id     TEXT NOT NULL REFERENCES tenants(id),
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(binary_id, user_id, tenant_id)
);
CREATE INDEX IF NOT EXISTS idx_scuc_tenant ON secure_cli_user_credentials(tenant_id);
CREATE INDEX IF NOT EXISTS idx_scuc_binary ON secure_cli_user_credentials(binary_id);

CREATE TABLE IF NOT EXISTS vault_documents (
    id           TEXT NOT NULL PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id     TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    scope        TEXT NOT NULL DEFAULT 'personal',
    path         TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    doc_type     TEXT NOT NULL DEFAULT 'note',
    content_hash TEXT NOT NULL DEFAULT '',
    summary      TEXT NOT NULL DEFAULT '',
    metadata     TEXT DEFAULT '{}',
    created_at   TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at   TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(agent_id, scope, path)
);
CREATE INDEX IF NOT EXISTS idx_vault_docs_tenant ON vault_documents(tenant_id);
CREATE INDEX IF NOT EXISTS idx_vault_docs_agent_scope ON vault_documents(agent_id, scope);
CREATE INDEX IF NOT EXISTS idx_vault_docs_type ON vault_documents(agent_id, doc_type);
CREATE INDEX IF NOT EXISTS idx_vault_docs_hash ON vault_documents(content_hash);

CREATE TABLE IF NOT EXISTS vault_links (
    id          TEXT NOT NULL PRIMARY KEY,
    from_doc_id TEXT NOT NULL REFERENCES vault_documents(id) ON DELETE CASCADE,
    to_doc_id   TEXT NOT NULL REFERENCES vault_documents(id) ON DELETE CASCADE,
    link_type   TEXT NOT NULL DEFAULT 'wikilink',
    context     TEXT NOT NULL DEFAULT '',
    created_at  TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(from_doc_id, to_doc_id, link_type)
);
CREATE INDEX IF NOT EXISTS idx_vault_links_from ON vault_links(from_doc_id);
CREATE INDEX IF NOT EXISTS idx_vault_links_to ON vault_links(to_doc_id);`,

	// Version 9 → 10: originally added summary column to vault_documents.
	// Now a no-op: migration 8 already creates vault_documents WITH summary.
	// DBs from schema.sql also include summary. ALTER would fail with "duplicate column".
	9: `SELECT 1;`,

	// Version 10 → 11: add team_id + custom_scope to vault_documents (fix cross-team UNIQUE),
	// add custom_scope to 8 other tables (vault_versions absent in SQLite).
	10: `-- Recreate vault_documents with team_id + custom_scope columns.
-- SQLite prohibits expressions (COALESCE) in UNIQUE constraints,
-- so we use a unique INDEX instead of inline UNIQUE.
CREATE TABLE vault_documents_new (
    id           TEXT NOT NULL PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id     TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    team_id      TEXT REFERENCES agent_teams(id) ON DELETE SET NULL,
    scope        TEXT NOT NULL DEFAULT 'personal',
    custom_scope TEXT,
    path         TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    doc_type     TEXT NOT NULL DEFAULT 'note',
    content_hash TEXT NOT NULL DEFAULT '',
    summary      TEXT NOT NULL DEFAULT '',
    metadata     TEXT DEFAULT '{}',
    created_at   TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at   TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
INSERT INTO vault_documents_new (id, tenant_id, agent_id, team_id, scope, custom_scope, path, title, doc_type, content_hash, summary, metadata, created_at, updated_at)
    SELECT id, tenant_id, agent_id, NULL, scope, NULL, path, title, doc_type, content_hash, summary, metadata, created_at, updated_at
    FROM vault_documents;
DROP TABLE vault_documents;
ALTER TABLE vault_documents_new RENAME TO vault_documents;
CREATE UNIQUE INDEX IF NOT EXISTS idx_vault_docs_unique_path
    ON vault_documents(agent_id, COALESCE(team_id, ''), scope, path);
CREATE INDEX IF NOT EXISTS idx_vault_docs_tenant ON vault_documents(tenant_id);
CREATE INDEX IF NOT EXISTS idx_vault_docs_agent_scope ON vault_documents(agent_id, scope);
CREATE INDEX IF NOT EXISTS idx_vault_docs_type ON vault_documents(agent_id, doc_type);
CREATE INDEX IF NOT EXISTS idx_vault_docs_hash ON vault_documents(content_hash);
CREATE INDEX IF NOT EXISTS idx_vault_docs_team ON vault_documents(team_id);
-- custom_scope on other tables (vault_versions absent in SQLite).
ALTER TABLE vault_links ADD COLUMN custom_scope TEXT;
ALTER TABLE memory_documents ADD COLUMN custom_scope TEXT;
ALTER TABLE memory_chunks ADD COLUMN custom_scope TEXT;
ALTER TABLE team_tasks ADD COLUMN custom_scope TEXT;
ALTER TABLE team_task_attachments ADD COLUMN custom_scope TEXT;
ALTER TABLE team_task_comments ADD COLUMN custom_scope TEXT;
ALTER TABLE team_task_events ADD COLUMN custom_scope TEXT;
ALTER TABLE subagent_tasks ADD COLUMN custom_scope TEXT;`,
	// Version 11 → 12: seed AGENTS_CORE.md + AGENTS_TASK.md, remove AGENTS_MINIMAL.md.
	11: `INSERT INTO agent_context_files (id, agent_id, file_name, content, tenant_id, created_at, updated_at)
SELECT lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)),2) || '-' || substr('89ab', abs(random()) % 4 + 1, 1) || substr(hex(randomblob(2)),2) || '-' || hex(randomblob(6))),
  a.id, 'AGENTS_CORE.md',
  '# Operating Rules (Core)

## Language & Communication

- Match the user''s language. Detect from first message, stay consistent.

## Internal Messages

- [System Message] blocks are internal context. Not user-visible.
- Rewrite system messages in your normal voice before delivering.
- Never use exec or curl for messaging.
- When asked to save or remember, MUST call write_file or edit in THIS turn.
',
  a.tenant_id, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM agents a
WHERE a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM agent_context_files WHERE agent_id = a.id AND file_name = 'AGENTS_CORE.md');

INSERT INTO agent_context_files (id, agent_id, file_name, content, tenant_id, created_at, updated_at)
SELECT lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)),2) || '-' || substr('89ab', abs(random()) % 4 + 1, 1) || substr(hex(randomblob(2)),2) || '-' || hex(randomblob(6))),
  a.id, 'AGENTS_TASK.md',
  '# Operating Rules (Task)

## Language & Communication

- Match the user''s language. Detect from first message, stay consistent.

## Internal Messages

- [System Message] blocks are internal context. Not user-visible.
- Rewrite system messages in your normal voice before delivering.
- Never use exec or curl for messaging.
- When asked to save or remember, MUST call write_file or edit in THIS turn.

## Memory

- Use memory_search before answering about prior work, decisions, or preferences.
- Use write_file to persist important information. No mental notes.
- Only reference MEMORY.md content in private/direct chats.

## Scheduling

- Use cron tool for periodic or timed tasks.
- Use kind: at for one-shot reminders.
',
  a.tenant_id, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM agents a
WHERE a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM agent_context_files WHERE agent_id = a.id AND file_name = 'AGENTS_TASK.md');

DELETE FROM agent_context_files WHERE file_name = 'AGENTS_MINIMAL.md';`,

	// Version 12 → 13: Phase 10 dreaming weighted scoring signals on
	// episodic_summaries. Mirrors PG migration 000045.
	12: `ALTER TABLE episodic_summaries ADD COLUMN recall_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE episodic_summaries ADD COLUMN recall_score REAL NOT NULL DEFAULT 0;
ALTER TABLE episodic_summaries ADD COLUMN last_recalled_at TEXT;
CREATE INDEX IF NOT EXISTS idx_episodic_recall_unpromoted ON episodic_summaries(agent_id, user_id, recall_score DESC)
    WHERE promoted_at IS NULL;`,

	// Version 13 → 14: vault_documents agent_id nullable + unique index with tenant_id.
	// SQLite requires table recreation to drop NOT NULL. Preserve all data.
	13: `CREATE TABLE vault_documents_new (
    id           TEXT NOT NULL PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id     TEXT REFERENCES agents(id) ON DELETE SET NULL,
    team_id      TEXT REFERENCES agent_teams(id) ON DELETE SET NULL,
    scope        TEXT NOT NULL DEFAULT 'personal',
    custom_scope TEXT,
    path         TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    doc_type     TEXT NOT NULL DEFAULT 'note',
    content_hash TEXT NOT NULL DEFAULT '',
    summary      TEXT NOT NULL DEFAULT '',
    metadata     TEXT DEFAULT '{}',
    created_at   TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at   TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
INSERT INTO vault_documents_new SELECT * FROM vault_documents;
DROP TABLE vault_documents;
ALTER TABLE vault_documents_new RENAME TO vault_documents;
DROP INDEX IF EXISTS idx_vault_docs_unique_path;
CREATE UNIQUE INDEX idx_vault_docs_unique_path
    ON vault_documents(tenant_id, COALESCE(agent_id, ''), COALESCE(team_id, ''), scope, path);
CREATE INDEX IF NOT EXISTS idx_vault_docs_tenant ON vault_documents(tenant_id);
CREATE INDEX IF NOT EXISTS idx_vault_docs_agent_scope ON vault_documents(agent_id, scope);
CREATE INDEX IF NOT EXISTS idx_vault_docs_type ON vault_documents(agent_id, doc_type);
CREATE INDEX IF NOT EXISTS idx_vault_docs_hash ON vault_documents(content_hash);
CREATE INDEX IF NOT EXISTS idx_vault_docs_team ON vault_documents(team_id);`,

	// Version 14 → 15: cron_jobs UNIQUE constraint on (agent_id, tenant_id, name).
	// Dedup first: keep one row per combo (SQLite has no DISTINCT ON — use GROUP BY + MIN).
	14: `DELETE FROM cron_jobs WHERE id NOT IN (
  SELECT MIN(id) FROM cron_jobs GROUP BY agent_id, tenant_id, name
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_cron_jobs_agent_tenant_name
  ON cron_jobs(agent_id, tenant_id, name);`,

	// Version 15 → 16: vault media linking schema
	// (team_task_attachments.base_name, vault_documents.path_basename,
	//  vault_links.metadata, auto-linking indexes).
	// Backfill of base_name / path_basename happens in backfillV16 below
	// (Go-loop — SQLite has no regexp_replace in modernc.org/sqlite bundle).
	15: `ALTER TABLE team_task_attachments ADD COLUMN base_name TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_tta_tenant_basename
  ON team_task_attachments(tenant_id, base_name);

ALTER TABLE vault_documents ADD COLUMN path_basename TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_vault_docs_basename
  ON vault_documents(tenant_id, path_basename);

ALTER TABLE vault_links ADD COLUMN metadata TEXT NOT NULL DEFAULT '{}';
CREATE INDEX IF NOT EXISTS idx_vault_links_source
  ON vault_links(json_extract(metadata, '$.source'))
  WHERE json_extract(metadata, '$.source') IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_vault_docs_delegation
  ON vault_documents(json_extract(metadata, '$.delegation_id'))
  WHERE json_extract(metadata, '$.delegation_id') IS NOT NULL;`,

	// Version 16 → 17: path prefix index for vault tree lazy-load queries.
	16: `CREATE INDEX IF NOT EXISTS idx_vault_docs_path_prefix ON vault_documents(tenant_id, path);`,

	// Version 17 → 18: listen_raw_messages table for WhatsApp listen-only mode.
	17: `CREATE TABLE IF NOT EXISTS listen_raw_messages (
    id            TEXT NOT NULL PRIMARY KEY,
    channel_name  VARCHAR(100) NOT NULL,
    chat_id       VARCHAR(255) NOT NULL,
    chat_name     VARCHAR(255) NOT NULL DEFAULT '',
    graph_id      VARCHAR(255) NOT NULL,
    sender        VARCHAR(255) NOT NULL DEFAULT '',
    sender_id     VARCHAR(255) NOT NULL DEFAULT '',
    body          TEXT NOT NULL DEFAULT '',
    msg_timestamp TEXT NOT NULL,
    agent_id      TEXT NOT NULL,
    tenant_id     TEXT NOT NULL REFERENCES tenants(id),
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    processed_at  TEXT
);
CREATE INDEX IF NOT EXISTS idx_listen_raw_agent_chat ON listen_raw_messages(agent_id, chat_id, created_at);
CREATE INDEX IF NOT EXISTS idx_listen_raw_pending ON listen_raw_messages(processed_at) WHERE processed_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_listen_raw_tenant ON listen_raw_messages(tenant_id);`,

	// Version 18 → 19: add system_prompt_preview to spans for debugging full system prompt.
	18: `ALTER TABLE spans ADD COLUMN system_prompt_preview TEXT;`,

	// Version 19 → 20: add event_time to kg_entities for temporal event correlation.
	19: `ALTER TABLE kg_entities ADD COLUMN event_time TEXT;
CREATE INDEX IF NOT EXISTS idx_kg_entities_event_time ON kg_entities(agent_id, user_id, event_time) WHERE event_time IS NOT NULL;`,

	// Version 20 → 21: add media_refs to listen_raw_messages for persisted media attachments.
	20: `ALTER TABLE listen_raw_messages ADD COLUMN media_refs TEXT NOT NULL DEFAULT '[]';`,

	// Version 21 → 22: seed STT builtin_tools row.
	21: `INSERT INTO builtin_tools (name, display_name, description, category, enabled, settings)
VALUES ('stt', 'Speech-to-Text', 'Transcribe voice/audio messages to text using ElevenLabs Scribe or a proxy service', 'media', 1, '{}')
ON CONFLICT (name) DO NOTHING;`,

	// Version 22 → 23: backfill mode: "cache-ttl" for agents with custom
	// context_pruning config missing the mode field. Mirrors PG migration 51.
	22: `UPDATE agents
SET context_pruning = json_set(context_pruning, '$.mode', 'cache-ttl')
WHERE context_pruning IS NOT NULL
  AND context_pruning <> ''
  AND context_pruning <> '{}'
  AND json_valid(context_pruning)
  AND json_type(context_pruning) = 'object'
  AND json_extract(context_pruning, '$.mode') IS NULL;`,

	// Version 23 → 24: hooks system (mirrors PG migrations 000052–000055).
	23: addHooksTables,

	// Version 24 → 25: vault_documents scope/ownership consistency triggers.
	// Mirrors PG migration 000055 CHECK constraint; SQLite cannot add CHECK via
	// ALTER TABLE so we use BEFORE INSERT + BEFORE UPDATE triggers instead.
	24: `CREATE TRIGGER IF NOT EXISTS trg_vault_docs_scope_consistency_ins
  BEFORE INSERT ON vault_documents
  FOR EACH ROW
  WHEN NOT (
    (NEW.scope='personal' AND NEW.agent_id IS NOT NULL AND NEW.team_id IS NULL) OR
    (NEW.scope='team'     AND NEW.team_id  IS NOT NULL AND NEW.agent_id IS NULL) OR
    (NEW.scope='shared'   AND NEW.agent_id IS NULL     AND NEW.team_id  IS NULL) OR
    NEW.scope='custom'
  )
  BEGIN
    SELECT RAISE(ABORT, 'vault_documents_scope_consistency violation');
  END;

CREATE TRIGGER IF NOT EXISTS trg_vault_docs_scope_consistency_upd
  BEFORE UPDATE OF scope, agent_id, team_id ON vault_documents
  FOR EACH ROW
  WHEN NOT (
    (NEW.scope='personal' AND NEW.agent_id IS NOT NULL AND NEW.team_id IS NULL) OR
    (NEW.scope='team'     AND NEW.team_id  IS NOT NULL AND NEW.agent_id IS NULL) OR
    (NEW.scope='shared'   AND NEW.agent_id IS NULL     AND NEW.team_id  IS NULL) OR
    NEW.scope='custom'
  )
  BEGIN
    SELECT RAISE(ABORT, 'vault_documents_scope_consistency violation');
  END;`,

	// Version 24 → 25: add chat_id column + composite index (mirrors PG migration 000056).
	// SQLite lacks regex by default — skip backfill (desktop is single-user; cross-chat risk minimal).
	25: `ALTER TABLE vault_documents ADD COLUMN chat_id TEXT;
CREATE INDEX IF NOT EXISTS idx_vault_docs_team_chat ON vault_documents(team_id, chat_id) WHERE team_id IS NOT NULL;`,

		// Version 25 → 26: change agent_heartbeats.provider_id FK to ON DELETE SET NULL
	// (mirrors PG migration 000057). SQLite cannot ALTER FK clauses, so the table
	// must be rebuilt. Explicit 25-column INSERT/SELECT to avoid silent column drift.
	26: `-- Defensive: clear orphan provider_id refs before rebuild (idempotent).
UPDATE agent_heartbeats
   SET provider_id = NULL
 WHERE provider_id IS NOT NULL
   AND provider_id NOT IN (SELECT id FROM llm_providers);

-- Rebuild table with ON DELETE SET NULL on provider_id FK.
CREATE TABLE agent_heartbeats_new (
    id                 TEXT NOT NULL PRIMARY KEY,
    agent_id           TEXT NOT NULL UNIQUE REFERENCES agents(id) ON DELETE CASCADE,
    enabled            BOOLEAN NOT NULL DEFAULT 0,
    interval_sec       INT NOT NULL DEFAULT 1800,
    prompt             TEXT,
    provider_id        TEXT REFERENCES llm_providers(id) ON DELETE SET NULL,
    model              VARCHAR(200),
    isolated_session   BOOLEAN NOT NULL DEFAULT 1,
    light_context      BOOLEAN NOT NULL DEFAULT 0,
    ack_max_chars      INT NOT NULL DEFAULT 300,
    max_retries        INT NOT NULL DEFAULT 2,
    active_hours_start VARCHAR(5),
    active_hours_end   VARCHAR(5),
    timezone           TEXT,
    channel            VARCHAR(50),
    chat_id            TEXT,
    next_run_at        TEXT,
    last_run_at        TEXT,
    last_status        VARCHAR(20),
    last_error         TEXT,
    run_count          INT NOT NULL DEFAULT 0,
    suppress_count     INT NOT NULL DEFAULT 0,
    metadata           TEXT DEFAULT '{}',
    created_at         TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at         TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO agent_heartbeats_new (
    id, agent_id, enabled, interval_sec, prompt, provider_id, model,
    isolated_session, light_context, ack_max_chars, max_retries,
    active_hours_start, active_hours_end, timezone, channel, chat_id,
    next_run_at, last_run_at, last_status, last_error,
    run_count, suppress_count, metadata, created_at, updated_at
) SELECT
    id, agent_id, enabled, interval_sec, prompt, provider_id, model,
    isolated_session, light_context, ack_max_chars, max_retries,
    active_hours_start, active_hours_end, timezone, channel, chat_id,
    next_run_at, last_run_at, last_status, last_error,
    run_count, suppress_count, metadata, created_at, updated_at
  FROM agent_heartbeats;

DROP TABLE agent_heartbeats;
ALTER TABLE agent_heartbeats_new RENAME TO agent_heartbeats;

-- Recreate the only index on agent_heartbeats (verified via grep).
CREATE INDEX IF NOT EXISTS idx_heartbeats_due
  ON agent_heartbeats(next_run_at)
  WHERE enabled = 1 AND next_run_at IS NOT NULL;`,

		// Version 27 → 28: MCP health checks table (mirrors PG migration 000061).
	27: `CREATE TABLE IF NOT EXISTS mcp_health_checks (
    id              TEXT NOT NULL PRIMARY KEY,
    server_id       TEXT NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    server_name     TEXT NOT NULL,
    tenant_id       TEXT NOT NULL,
    status          TEXT NOT NULL,
    latency_ms      INTEGER,
    error           TEXT,
    tool_count      INTEGER DEFAULT 0,
    health_failures INTEGER DEFAULT 0,
    checked_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_mcp_health_server_time ON mcp_health_checks(server_id, checked_at DESC);
CREATE INDEX IF NOT EXISTS idx_mcp_health_tenant_time ON mcp_health_checks(tenant_id, checked_at DESC);`,
		// Version 28 → 29: add embedded_at column to listen_raw_messages (mirrors PG migration 000064).
		28: `ALTER TABLE listen_raw_messages ADD COLUMN embedded_at TEXT;`,
		// Version 29 → 30: add extraction tracking columns to listen_raw_messages (mirrors PG migration 000062).
		29: `ALTER TABLE listen_raw_messages ADD COLUMN extraction_status VARCHAR(20) NOT NULL DEFAULT 'pending';
	ALTER TABLE listen_raw_messages ADD COLUMN extraction_error TEXT;
	ALTER TABLE listen_raw_messages ADD COLUMN extraction_attempts INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE listen_raw_messages ADD COLUMN last_attempted_at TEXT;
	UPDATE listen_raw_messages SET extraction_status = 'extracted' WHERE processed_at IS NOT NULL;`,
		// Version 30 → 31: add embedded_chunks column to usage_snapshots (mirrors PG migration 000067).
		30: `ALTER TABLE usage_snapshots ADD COLUMN embedded_chunks INTEGER NOT NULL DEFAULT 0;`,
		// Version 31 → 32: webhooks + webhook_calls tables.
		// scopes/ip_allowlist stored as JSON TEXT; bool columns as INTEGER (0/1).
		31: `CREATE TABLE IF NOT EXISTS webhooks (
    id                  TEXT        PRIMARY KEY,
    tenant_id           TEXT        NOT NULL,
    agent_id            TEXT        REFERENCES agents(id) ON DELETE SET NULL,
    name                TEXT        NOT NULL,
    kind                TEXT        NOT NULL CHECK (kind IN ('llm', 'message')),
    secret_prefix       TEXT,
    secret_hash         TEXT        NOT NULL,
    scopes              TEXT        NOT NULL DEFAULT '[]',
    channel_id          TEXT,
    rate_limit_per_min  INTEGER     NOT NULL DEFAULT 60,
    ip_allowlist        TEXT        NOT NULL DEFAULT '[]',
    require_hmac        INTEGER     NOT NULL DEFAULT 0,
    localhost_only      INTEGER     NOT NULL DEFAULT 0,
    revoked             INTEGER     NOT NULL DEFAULT 0,
    created_by          TEXT,
    created_at          TEXT        NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at          TEXT        NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_used_at        TEXT
);
CREATE INDEX IF NOT EXISTS idx_webhooks_tenant
    ON webhooks (tenant_id);
CREATE INDEX IF NOT EXISTS idx_webhooks_tenant_agent
    ON webhooks (tenant_id, agent_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_webhooks_secret
    ON webhooks (secret_hash)
    WHERE revoked = 0;
CREATE TABLE IF NOT EXISTS webhook_calls (
    id               TEXT     PRIMARY KEY,
    tenant_id        TEXT     NOT NULL,
    webhook_id       TEXT     NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    agent_id         TEXT,
    idempotency_key  TEXT,
    mode             TEXT     NOT NULL CHECK (mode IN ('sync', 'async')),
    callback_url     TEXT,
    status           TEXT     NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'failed', 'dead')),
    attempts         INTEGER  NOT NULL DEFAULT 0,
    delivery_id      TEXT     NOT NULL,
    next_attempt_at  TEXT,
    started_at       TEXT,
    request_payload  TEXT,
    response         TEXT,
    last_error       TEXT,
    created_at       TEXT     NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    completed_at     TEXT
);
CREATE INDEX IF NOT EXISTS idx_webhook_calls_tenant_created
    ON webhook_calls (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_webhook_calls_status_attempt
    ON webhook_calls (status, next_attempt_at);
CREATE UNIQUE INDEX IF NOT EXISTS uq_webhook_calls_idempotency
    ON webhook_calls (webhook_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;`,

		// Version 32 → 33: add lease_token to webhook_calls for optimistic-concurrency CAS.
		32: `ALTER TABLE webhook_calls ADD COLUMN lease_token TEXT;`,

		// Version 33 → 34: add encrypted_secret to webhooks (AES-256-GCM of raw secret).
		33: `ALTER TABLE webhooks ADD COLUMN encrypted_secret TEXT NOT NULL DEFAULT '';`,

		// Version 34 -> 35: add workstations and agent_workstation_links tables.
		34: `CREATE TABLE IF NOT EXISTS workstations (
			id              TEXT PRIMARY KEY,
			workstation_key VARCHAR(100) NOT NULL,
			tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
			name            VARCHAR(255) NOT NULL,
			backend_type    VARCHAR(20) NOT NULL CHECK (backend_type IN ('ssh','docker')),
			metadata        BLOB NOT NULL,
			default_cwd     VARCHAR(500) NOT NULL DEFAULT '',
			default_env     BLOB NOT NULL,
			active          INTEGER NOT NULL DEFAULT 1,
			created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			created_by      VARCHAR(255) NOT NULL DEFAULT '',
			UNIQUE (tenant_id, workstation_key)
		);
		CREATE INDEX IF NOT EXISTS idx_workstations_tenant_active
			ON workstations(tenant_id, active) WHERE active = 1;

		CREATE TABLE IF NOT EXISTS agent_workstation_links (
			agent_id        TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
			workstation_id  TEXT NOT NULL REFERENCES workstations(id) ON DELETE CASCADE,
			tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
			is_default      INTEGER NOT NULL DEFAULT 0,
			created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			PRIMARY KEY (agent_id, workstation_id)
		);
		CREATE INDEX IF NOT EXISTS idx_agent_workstation_tenant ON agent_workstation_links(tenant_id);`,

		// Version 35 -> 36: add workstation_permissions table.
		35: `CREATE TABLE IF NOT EXISTS workstation_permissions (
			id              TEXT PRIMARY KEY,
			workstation_id  TEXT NOT NULL REFERENCES workstations(id) ON DELETE CASCADE,
			tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
			pattern         VARCHAR(500) NOT NULL,
			enabled         INTEGER NOT NULL DEFAULT 1,
			created_by      VARCHAR(255) NOT NULL DEFAULT '',
			created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			UNIQUE (workstation_id, pattern)
		);
		CREATE INDEX IF NOT EXISTS idx_workstation_perms_ws ON workstation_permissions(workstation_id) WHERE enabled = 1;
		CREATE INDEX IF NOT EXISTS idx_workstation_perms_tenant ON workstation_permissions(tenant_id);`,

		// Version 36 -> 37: add workstation_activity audit log table.
		36: `CREATE TABLE IF NOT EXISTS workstation_activity (
			id              TEXT PRIMARY KEY,
			tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
			workstation_id  TEXT NOT NULL REFERENCES workstations(id) ON DELETE CASCADE,
			agent_id        VARCHAR(255) NOT NULL DEFAULT '',
			action          VARCHAR(20)  NOT NULL,
			cmd_hash        VARCHAR(64)  NOT NULL DEFAULT '',
			cmd_preview     VARCHAR(200) NOT NULL DEFAULT '',
			exit_code       INTEGER,
			duration_ms     INTEGER,
			deny_reason     VARCHAR(200) NOT NULL DEFAULT '',
			created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		);
		CREATE INDEX IF NOT EXISTS idx_ws_activity_ws_time     ON workstation_activity(workstation_id, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_ws_activity_tenant_time ON workstation_activity(tenant_id, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_ws_activity_retention   ON workstation_activity(created_at);`,

		// Version 37 → 38: add workstation_command_groups and workstation_group_permissions tables.
		37: `CREATE TABLE IF NOT EXISTS workstation_command_groups (
			id          TEXT PRIMARY KEY,
			tenant_id   TEXT REFERENCES tenants(id) ON DELETE CASCADE,
			name        VARCHAR(100) NOT NULL,
			description TEXT,
			patterns    TEXT NOT NULL DEFAULT '[]',
			is_builtin  INTEGER NOT NULL DEFAULT 0,
			created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			created_by  VARCHAR(100)
		);
		CREATE INDEX IF NOT EXISTS idx_workstation_cmd_groups_tenant ON workstation_command_groups(tenant_id);

		CREATE TABLE IF NOT EXISTS workstation_group_permissions (
			id             TEXT PRIMARY KEY,
			workstation_id TEXT NOT NULL REFERENCES workstations(id) ON DELETE CASCADE,
			group_id       TEXT NOT NULL REFERENCES workstation_command_groups(id) ON DELETE CASCADE,
			tenant_id      TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
			enabled        INTEGER NOT NULL DEFAULT 1,
			created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			UNIQUE (workstation_id, group_id)
		);
		CREATE INDEX IF NOT EXISTS idx_workstation_group_perms_ws    ON workstation_group_permissions(workstation_id);
		CREATE INDEX IF NOT EXISTS idx_workstation_group_perms_group ON workstation_group_permissions(group_id);

		INSERT OR IGNORE INTO workstation_command_groups (id, tenant_id, name, description, patterns, is_builtin, created_by) VALUES
		  ('0193a5b0-7000-7000-8000-000000000100', NULL, 'Linux Monitoring', 'Common system monitoring commands', '["ps","top","free","df","htop","lsblk","vmstat","iostat","netstat","ss"]', 1, 'system'),
		  ('0193a5b0-7000-7000-8000-000000000101', NULL, 'Container Tools', 'Docker and Kubernetes utilities', '["docker","kubectl","k9s","helm","ctr","nerdctl"]', 1, 'system'),
		  ('0193a5b0-7000-7000-8000-000000000102', NULL, 'System Utilities', 'Service and log management', '["systemctl","journalctl","service","timedatectl","hostnamectl"]', 1, 'system'),
		  ('0193a5b0-7000-7000-8000-000000000103', NULL, 'Network Tools', 'Network diagnostics', '["ping","traceroute","curl","wget","nslookup","dig","ss","ip"]', 1, 'system');`,

		// Version 38 → 39: add Default Commands built-in group.
		38: `INSERT OR IGNORE INTO workstation_command_groups (id, tenant_id, name, description, patterns, is_builtin, created_by) VALUES
		  ('0193a5b0-7000-7000-8000-000000000104', NULL, 'Default Commands', 'Safe commands automatically allowed on all workstations', '["echo","pwd","ls","cat","git","env","whoami","hostname","date","uname","claude","cd","pushd","popd","dirs","printf","read","export","unset","declare","typeset","local","test","true","false","set","shift","exit","wait","source",".","type","help","history","times","builtin","command","shopt","ulimit","umask","mapfile","readarray","caller","enable","compgen","complete","compopt"]', 1, 'system');`,

		// Version 39 → 40: add agent_id index on workstation_activity for filter support.
		39: `CREATE INDEX IF NOT EXISTS idx_ws_activity_agent_time ON workstation_activity(agent_id, created_at DESC);`,

		// Version 40 → 41: multi-auth module tables (mirrors PG migration 000079).
		// users, user_identities, groups, group_members, join_requests, audit_log, refresh_tokens.
		40: `CREATE TABLE IF NOT EXISTS users (
	    id               TEXT NOT NULL PRIMARY KEY,
	    email            VARCHAR(255) NOT NULL,
	    display_name     VARCHAR(255) NOT NULL,
	    avatar_url       TEXT,
	    tenant_id        TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
	    auth_provider    VARCHAR(20) NOT NULL CHECK (auth_provider IN ('local', 'entra_id', 'google')),
	    password_hash    TEXT,
	    status           VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'deactivated')),
	    last_login_at    TEXT,
	    created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	    updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	    UNIQUE(tenant_id, email)
	);
	CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
	CREATE INDEX IF NOT EXISTS idx_users_tenant ON users(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_users_tenant_email ON users(tenant_id, email);
	CREATE INDEX IF NOT EXISTS idx_users_status ON users(tenant_id, status);

	CREATE TABLE IF NOT EXISTS user_identities (
	    id               TEXT NOT NULL PRIMARY KEY,
	    user_id          TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	    provider         VARCHAR(20) NOT NULL CHECK (provider IN ('local', 'entra_id', 'google')),
	    provider_subject VARCHAR(255) NOT NULL,
	    provider_tenant  VARCHAR(255),
	    email            VARCHAR(255) NOT NULL,
	    linked_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	    last_used_at     TEXT,
	    UNIQUE(user_id, provider),
	    UNIQUE(provider, provider_subject)
	);
	CREATE INDEX IF NOT EXISTS idx_user_identities_user ON user_identities(user_id);
	CREATE INDEX IF NOT EXISTS idx_user_identities_email ON user_identities(email);
	CREATE INDEX IF NOT EXISTS idx_user_identities_lookup ON user_identities(provider, provider_subject);

	CREATE TABLE IF NOT EXISTS groups (
	    id              TEXT NOT NULL PRIMARY KEY,
	    name            VARCHAR(255) NOT NULL,
	    slug            VARCHAR(100) NOT NULL,
	    description     TEXT,
	    parent_group_id TEXT REFERENCES groups(id) ON DELETE SET NULL,
	    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
	    visibility      VARCHAR(20) NOT NULL DEFAULT 'closed' CHECK (visibility IN ('open', 'closed')),
	    max_members     INTEGER NOT NULL DEFAULT 0,
	    created_by      TEXT NOT NULL REFERENCES users(id),
	    status          VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'deleted')),
	    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	    UNIQUE(slug, tenant_id)
	);
	CREATE INDEX IF NOT EXISTS idx_groups_slug ON groups(slug);
	CREATE INDEX IF NOT EXISTS idx_groups_tenant ON groups(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_groups_parent ON groups(parent_group_id);
	CREATE INDEX IF NOT EXISTS idx_groups_tenant_status ON groups(tenant_id, status) WHERE status = 'active';

	CREATE TABLE IF NOT EXISTS group_members (
	    id         TEXT NOT NULL PRIMARY KEY,
	    group_id   TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	    role       VARCHAR(20) NOT NULL DEFAULT 'member' CHECK (role IN ('admin', 'member')),
	    joined_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	    joined_via VARCHAR(20) NOT NULL DEFAULT 'admin_add' CHECK (joined_via IN ('admin_add', 'self_join', 'request_approved')),
	    UNIQUE(group_id, user_id)
	);
	CREATE INDEX IF NOT EXISTS idx_group_members_group ON group_members(group_id);
	CREATE INDEX IF NOT EXISTS idx_group_members_user ON group_members(user_id);
	CREATE INDEX IF NOT EXISTS idx_group_members_group_role ON group_members(group_id, role);

	CREATE TABLE IF NOT EXISTS join_requests (
	    id           TEXT NOT NULL PRIMARY KEY,
	    group_id     TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	    status       VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
	    reviewed_by  TEXT REFERENCES users(id),
	    reviewed_at  TEXT,
	    message      TEXT,
	    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	    UNIQUE(group_id, user_id, status)
	);
	CREATE INDEX IF NOT EXISTS idx_join_requests_group_status ON join_requests(group_id, status);
	CREATE INDEX IF NOT EXISTS idx_join_requests_user ON join_requests(user_id);

	CREATE TABLE IF NOT EXISTS audit_log (
	    id             TEXT NOT NULL PRIMARY KEY,
	    tenant_id      TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
	    actor_id       TEXT NOT NULL REFERENCES users(id),
	    action         VARCHAR(100) NOT NULL,
	    resource_type  VARCHAR(20) NOT NULL CHECK (resource_type IN ('user', 'group', 'membership', 'permission', 'system')),
	    resource_id    TEXT NOT NULL,
	    group_id       TEXT REFERENCES groups(id) ON DELETE SET NULL,
	    detail         TEXT,
	    ip_address     VARCHAR(45),
	    user_agent     TEXT,
	    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	);
	CREATE INDEX IF NOT EXISTS idx_audit_log_tenant ON audit_log(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_audit_log_actor ON audit_log(tenant_id, actor_id);
	CREATE INDEX IF NOT EXISTS idx_audit_log_resource ON audit_log(tenant_id, resource_type, resource_id);
	CREATE INDEX IF NOT EXISTS idx_audit_log_group ON audit_log(tenant_id, group_id);
	CREATE INDEX IF NOT EXISTS idx_audit_log_created ON audit_log(tenant_id, created_at DESC);

	CREATE TABLE IF NOT EXISTS refresh_tokens (
	    id          TEXT NOT NULL PRIMARY KEY,
	    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	    token_hash  VARCHAR(64) NOT NULL UNIQUE,
	    device_info TEXT,
	    expires_at  TEXT NOT NULL,
	    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	);
	CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id);
	CREATE INDEX IF NOT EXISTS idx_refresh_tokens_hash ON refresh_tokens(token_hash);
	CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires ON refresh_tokens(expires_at);`,

	// Version 41 → 42: drop is_tenant_admin from users (roles now in tenant_users).
	41: `ALTER TABLE users DROP COLUMN IF EXISTS is_tenant_admin;`,

	// Version 42 → 43: drop tenant_id from users; users becomes global identity table.
	42: `INSERT INTO tenant_users (tenant_id, user_id, role, metadata, created_at, updated_at)
SELECT u.tenant_id, u.id, 'member', '{}', datetime('now'), datetime('now')
FROM users u
WHERE u.tenant_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM tenant_users tu
      WHERE tu.tenant_id = u.tenant_id AND tu.user_id = u.id
  );
ALTER TABLE users DROP COLUMN tenant_id;
DROP INDEX IF EXISTS idx_users_tenant;
DROP INDEX IF EXISTS idx_users_tenant_email;
DROP INDEX IF EXISTS idx_users_status;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email);`,

	// Version 43 → 44: user/group/role refactor.
	// Adds roles, role_permissions, user_roles, group_roles.
	// Replaces tenant_users.role with is_owner and many-to-many user_roles.
	// Removes group_members.role.
	43: `ALTER TABLE tenant_users ADD COLUMN is_owner INTEGER NOT NULL DEFAULT 0;
UPDATE tenant_users SET is_owner = 1 WHERE role = 'owner';

CREATE TABLE IF NOT EXISTS roles (
    id          TEXT NOT NULL PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    is_system   INTEGER NOT NULL DEFAULT 0,
    permissions TEXT NOT NULL DEFAULT '[]',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(tenant_id, name)
);
CREATE INDEX IF NOT EXISTS idx_roles_tenant ON roles(tenant_id);

CREATE TABLE IF NOT EXISTS role_permissions (
    id          TEXT NOT NULL PRIMARY KEY,
    role_id     TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission  VARCHAR(100) NOT NULL,
    UNIQUE(role_id, permission)
);
CREATE INDEX IF NOT EXISTS idx_role_permissions_role ON role_permissions(role_id);

CREATE TABLE IF NOT EXISTS user_roles (
    id          TEXT NOT NULL PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id     VARCHAR(255) NOT NULL,
    role_id     TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    UNIQUE(tenant_id, user_id, role_id)
);
CREATE INDEX IF NOT EXISTS idx_user_roles_user_tenant ON user_roles(user_id, tenant_id);
CREATE INDEX IF NOT EXISTS idx_user_roles_role ON user_roles(role_id);

CREATE TABLE IF NOT EXISTS group_roles (
    id          TEXT NOT NULL PRIMARY KEY,
    group_id    TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    role_id     TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    UNIQUE(group_id, role_id)
);
CREATE INDEX IF NOT EXISTS idx_group_roles_group ON group_roles(group_id);
CREATE INDEX IF NOT EXISTS idx_group_roles_role ON group_roles(role_id);

-- Default system roles per tenant
INSERT INTO roles (id, tenant_id, name, description, is_system, permissions, created_at, updated_at)
SELECT lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)),2) || '-' || substr('89ab', abs(random()) % 4 + 1, 1) || substr(hex(randomblob(2)),2) || '-' || hex(randomblob(6))),
       t.id, 'Admin', 'Full tenant administration', 1,
       '["user.list","user.get","user.create","user.update","user.delete","user.enroll","user.unenroll","user.assign_role","user.pre_provision","user.suspend","user.deactivate","group.list","group.get","group.create","group.update","group.delete","group.manage_members","group.assign_role","role.list","role.get","role.create","role.update","role.delete","audit.view_all","system.manage_settings","system.manage_auth","system.view_health"]',
       strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM tenants t
WHERE NOT EXISTS (SELECT 1 FROM roles r WHERE r.tenant_id = t.id AND r.name = 'Admin');

INSERT INTO roles (id, tenant_id, name, description, is_system, permissions, created_at, updated_at)
SELECT lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)),2) || '-' || substr('89ab', abs(random()) % 4 + 1, 1) || substr(hex(randomblob(2)),2) || '-' || hex(randomblob(6))),
       t.id, 'Member', 'Regular member', 1,
       '["group.list","group.get","group.view_hierarchy","artifact.upload_personal","artifact.submit_review","agent.create_personal","artifact.view_group","artifact.view_tenant","artifact.delete_own"]',
       strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM tenants t
WHERE NOT EXISTS (SELECT 1 FROM roles r WHERE r.tenant_id = t.id AND r.name = 'Member');

INSERT INTO roles (id, tenant_id, name, description, is_system, permissions, created_at, updated_at)
SELECT lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)),2) || '-' || substr('89ab', abs(random()) % 4 + 1, 1) || substr(hex(randomblob(2)),2) || '-' || hex(randomblob(6))),
       t.id, 'Viewer', 'Read-only access', 1,
       '["group.list","group.get","artifact.view_group","artifact.view_tenant"]',
       strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM tenants t
WHERE NOT EXISTS (SELECT 1 FROM roles r WHERE r.tenant_id = t.id AND r.name = 'Viewer');

-- Backfill user_roles from old tenant_users.role
INSERT INTO user_roles (id, tenant_id, user_id, role_id)
SELECT lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)),2) || '-' || substr('89ab', abs(random()) % 4 + 1, 1) || substr(hex(randomblob(2)),2) || '-' || hex(randomblob(6))),
       tu.tenant_id, tu.user_id,
       COALESCE(
         (SELECT id FROM roles WHERE tenant_id = tu.tenant_id AND name = CASE tu.role
           WHEN 'admin' THEN 'Admin'
           WHEN 'operator' THEN 'Member'
           WHEN 'member' THEN 'Member'
           WHEN 'viewer' THEN 'Viewer'
           ELSE 'Member'
         END),
         (SELECT id FROM roles WHERE tenant_id = tu.tenant_id AND name = 'Member')
       )
FROM tenant_users tu
WHERE tu.role != 'owner' AND tu.role IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM user_roles ur
      WHERE ur.tenant_id = tu.tenant_id AND ur.user_id = tu.user_id
        AND ur.role_id = COALESCE(
             (SELECT id FROM roles WHERE tenant_id = tu.tenant_id AND name = CASE tu.role
               WHEN 'admin' THEN 'Admin'
               WHEN 'operator' THEN 'Member'
               WHEN 'member' THEN 'Member'
               WHEN 'viewer' THEN 'Viewer'
               ELSE 'Member'
             END),
             (SELECT id FROM roles WHERE tenant_id = tu.tenant_id AND name = 'Member')
           )
  );

-- Normalize role_permissions from denormalized cache
INSERT INTO role_permissions (id, role_id, permission)
SELECT lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)),2) || '-' || substr('89ab', abs(random()) % 4 + 1, 1) || substr(hex(randomblob(2)),2) || '-' || hex(randomblob(6))),
       r.id, json_each.value
FROM roles r, json_each(r.permissions)
WHERE NOT EXISTS (SELECT 1 FROM role_permissions rp WHERE rp.role_id = r.id AND rp.permission = json_each.value);

-- Drop old columns
ALTER TABLE tenant_users DROP COLUMN role;
ALTER TABLE group_members DROP COLUMN role;
DROP INDEX IF EXISTS idx_group_members_group_role;`,
		// Version 43 → 44: remove Operator system role, reassign users to Member.
		44: `-- Reassign Operator users to Member role (skip if already Member)
INSERT OR IGNORE INTO user_roles (id, tenant_id, user_id, role_id)
SELECT lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)),2) || '-' || substr('89ab', abs(random()) % 4 + 1, 1) || substr(hex(randomblob(2)),2) || '-' || hex(randomblob(6))),
       ur.tenant_id, ur.user_id,
       (SELECT id FROM roles WHERE tenant_id = ur.tenant_id AND name = 'Member' LIMIT 1)
FROM user_roles ur
JOIN roles r ON r.id = ur.role_id
WHERE r.name = 'Operator'
  AND NOT EXISTS (
    SELECT 1 FROM user_roles ur2
    JOIN roles r2 ON r2.id = ur2.role_id
    WHERE ur2.tenant_id = ur.tenant_id
      AND ur2.user_id = ur.user_id
      AND r2.name = 'Member'
  );

DELETE FROM role_permissions WHERE role_id IN (SELECT id FROM roles WHERE name = 'Operator');
DELETE FROM user_roles WHERE role_id IN (SELECT id FROM roles WHERE name = 'Operator');
DELETE FROM group_roles WHERE role_id IN (SELECT id FROM roles WHERE name = 'Operator');
DELETE FROM roles WHERE name = 'Operator';`,
	// Version 44 -> 45: add user.pre_provision, user.suspend, user.deactivate to existing Admin roles.
	// Mirrors PG migration 000085. Fresh DBs already include these in the seed (migration 43).
	45: `-- Add missing user lifecycle permissions to existing Admin roles.
-- These are required by route guards in internal/http/users.go but were
-- missing from the initial seed data (migration 43 only seeds NEW roles).

-- Step 1: Add permissions to role_permissions table (used by resolver)
INSERT OR IGNORE INTO role_permissions (id, role_id, permission)
SELECT lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)),2) || '-' || substr('89ab', abs(random()) % 4 + 1, 1) || substr(hex(randomblob(2)),2) || '-' || hex(randomblob(6))),
       r.id, p.value
FROM roles r, json_each('["user.pre_provision","user.suspend","user.deactivate"]') AS p
WHERE r.name = 'Admin'
  AND r.is_system = 1;

-- Step 2: Update denormalized roles.permissions JSON to match
UPDATE roles
SET permissions = (
    SELECT json_group_array(p2.permission)
    FROM (SELECT DISTINCT permission FROM role_permissions WHERE role_id = roles.id ORDER BY permission) p2
  ),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE name = 'Admin'
  AND is_system = 1;`,
		// Version 45 → 46: no-op placeholder (reserved).
		// Version 46 → 47: add phone column to users table (mirrors PG migration 000088).
		46: `ALTER TABLE users ADD COLUMN phone TEXT;`,
}

// addHooksTables is the SQLite incremental migration for schema v19 → v20.
// Mirrors PG migrations 000052–000055 (consolidated — desktop never shipped
// with intermediate agent_hooks / agent_hook_agents names).
const addHooksTables = `
CREATE TABLE IF NOT EXISTS hooks (
    id           TEXT NOT NULL PRIMARY KEY,
    tenant_id    TEXT NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001',
    scope        TEXT NOT NULL CHECK (scope IN ('global', 'tenant', 'agent')),
    event        TEXT NOT NULL,
    handler_type TEXT NOT NULL CHECK (handler_type IN ('command', 'http', 'prompt', 'script')),
    config       TEXT NOT NULL DEFAULT '{}',
    matcher      TEXT,
    if_expr      TEXT,
    timeout_ms   INTEGER NOT NULL DEFAULT 5000,
    on_timeout   TEXT NOT NULL DEFAULT 'block' CHECK (on_timeout IN ('block', 'allow')),
    priority     INTEGER NOT NULL DEFAULT 0,
    enabled      INTEGER NOT NULL DEFAULT 1,
    version      INTEGER NOT NULL DEFAULT 1,
    source       TEXT NOT NULL DEFAULT 'ui' CHECK (source IN ('ui', 'api', 'seed', 'builtin')),
    metadata     TEXT NOT NULL DEFAULT '{}',
    name         TEXT,
    created_by   TEXT,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_hooks_lookup
    ON hooks (tenant_id, event)
    WHERE enabled = 1;
CREATE TABLE IF NOT EXISTS hook_agents (
    hook_id  TEXT NOT NULL REFERENCES hooks(id) ON DELETE CASCADE,
    agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    PRIMARY KEY (hook_id, agent_id)
);
CREATE INDEX IF NOT EXISTS idx_hook_agents_agent
    ON hook_agents (agent_id);
CREATE TABLE IF NOT EXISTS hook_executions (
    id           TEXT NOT NULL PRIMARY KEY,
    hook_id      TEXT REFERENCES hooks(id) ON DELETE SET NULL,
    session_id   TEXT,
    event        TEXT NOT NULL,
    input_hash   TEXT,
    decision     TEXT NOT NULL CHECK (decision IN ('allow', 'block', 'error', 'timeout')),
    duration_ms  INTEGER NOT NULL DEFAULT 0,
    retry        INTEGER NOT NULL DEFAULT 0,
    dedup_key    TEXT,
    error        TEXT,
    error_detail BLOB,
    metadata     TEXT NOT NULL DEFAULT '{}',
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_hook_executions_session
    ON hook_executions (session_id, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS uq_hook_executions_dedup
    ON hook_executions (dedup_key)
    WHERE dedup_key IS NOT NULL;
CREATE TABLE IF NOT EXISTS tenant_hook_budget (
    tenant_id      TEXT NOT NULL PRIMARY KEY,
    month_start    TEXT NOT NULL,
    budget_total   INTEGER NOT NULL DEFAULT 0,
    remaining      INTEGER NOT NULL DEFAULT 0,
    last_warned_at TEXT,
    metadata       TEXT NOT NULL DEFAULT '{}',
    updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);`

// backfillV16 populates base_name / path_basename for rows that existed
// before the v15 → v16 migration. Idempotent — re-running on already-filled
// rows is a no-op thanks to the WHERE base_name = '' filter.
func backfillV16(ctx context.Context, db *sql.DB) error {
	type row struct{ id, path string }

	// ---- team_task_attachments ----
	attRows, err := collectIDPath(ctx, db,
		`SELECT id, path FROM team_task_attachments WHERE base_name = ''`)
	if err != nil {
		return fmt.Errorf("v16 scan attachments: %w", err)
	}
	if err := updateBaseNames(ctx, db,
		`UPDATE team_task_attachments SET base_name = ? WHERE id = ?`, attRows); err != nil {
		return fmt.Errorf("v16 update attachments: %w", err)
	}

	// ---- vault_documents ----
	docRows, err := collectIDPath(ctx, db,
		`SELECT id, path FROM vault_documents WHERE path_basename = ''`)
	if err != nil {
		return fmt.Errorf("v16 scan vault_docs: %w", err)
	}
	if err := updateBaseNames(ctx, db,
		`UPDATE vault_documents SET path_basename = ? WHERE id = ?`, docRows); err != nil {
		return fmt.Errorf("v16 update vault_docs: %w", err)
	}
	return nil
}

// collectIDPath reads (id, path) tuples for a SELECT that returns exactly
// those two columns. Separated so backfillV16 stays readable.
func collectIDPath(ctx context.Context, db *sql.DB, q string) ([][2]string, error) {
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][2]string
	for rows.Next() {
		var id, path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, err
		}
		out = append(out, [2]string{id, path})
	}
	return out, rows.Err()
}

// updateBaseNames runs a prepared UPDATE inside one transaction for all rows.
// No-op when rows is empty. The prepared statement form keeps SQLite happy
// on larger backfills (<= ~10k legacy rows on typical desktop lite DBs).
func updateBaseNames(ctx context.Context, db *sql.DB, updateSQL string, rows [][2]string) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, updateSQL)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		bn := ComputeAttachmentBaseName(r[1])
		if _, err := stmt.ExecContext(ctx, bn, r[0]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("update %s: %w", r[0], err)
		}
	}
	return tx.Commit()
}

// EnsureSchema creates tables if they don't exist and applies incremental migrations.
//
// Flow:
//  1. Fresh DB (no schema_version row) → apply full schema.sql + set version = SchemaVersion
//  2. Existing DB with version < SchemaVersion → apply patches sequentially
//  3. Existing DB with version == SchemaVersion → no-op
//  4. Always: seed master tenant (idempotent)
func EnsureSchema(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER NOT NULL PRIMARY KEY
	)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	var current int
	err := db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		// Fresh database — apply full schema.
		slog.Info("sqlite: applying initial schema", "version", SchemaVersion)
		tx, txErr := db.Begin()
		if txErr != nil {
			return fmt.Errorf("begin schema tx: %w", txErr)
		}
		if _, err := tx.Exec(schemaSQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply schema: %w", err)
		}
		if _, err := tx.Exec("INSERT INTO schema_version (version) VALUES (?)", SchemaVersion); err != nil {
			tx.Rollback()
			return fmt.Errorf("set schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit schema tx: %w", err)
		}
		return seedMasterTenant(db)
	}
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	// Apply incremental migrations for existing DBs.
	if current < SchemaVersion {
		slog.Info("sqlite: migrating schema", "from", current, "to", SchemaVersion)
		for v := current; v < SchemaVersion; v++ {
			patch, ok := migrations[v]
			if !ok {
				return fmt.Errorf("sqlite: missing migration for version %d → %d", v, v+1)
			}
			// Migrations that rebuild a table referenced by another table's FK
			// require foreign_keys=OFF per SQLite altertable §7. The pragma is
			// a no-op inside a transaction, so toggle it around BEGIN/COMMIT.
			// v26 → v27: rebuilds agent_heartbeats; heartbeat_run_logs.heartbeat_id FKs into it.
			needsFKOff := v == 26
			if needsFKOff {
				if _, err := db.Exec("PRAGMA foreign_keys=OFF"); err != nil {
					return fmt.Errorf("disable FK before v%d: %w", v, err)
				}
			}
			tx, txErr := db.Begin()
			if txErr != nil {
				if needsFKOff {
					_, _ = db.Exec("PRAGMA foreign_keys=ON")
				}
				return fmt.Errorf("begin migration tx v%d: %w", v, txErr)
			}
			if _, err := tx.Exec(patch); err != nil {
				tx.Rollback()
				if needsFKOff {
					_, _ = db.Exec("PRAGMA foreign_keys=ON")
				}
				return fmt.Errorf("apply migration v%d: %w", v, err)
			}
			if _, err := tx.Exec(
				"UPDATE schema_version SET version = ? WHERE version = ?", v+1, v,
			); err != nil {
				tx.Rollback()
				if needsFKOff {
					_, _ = db.Exec("PRAGMA foreign_keys=ON")
				}
				return fmt.Errorf("update schema version v%d: %w", v, err)
			}
			if err := tx.Commit(); err != nil {
				if needsFKOff {
					_, _ = db.Exec("PRAGMA foreign_keys=ON")
				}
				return fmt.Errorf("commit migration v%d: %w", v, err)
			}
			if needsFKOff {
				// Verify referential integrity after the rebuild.
				if rows, qErr := db.Query("PRAGMA foreign_key_check"); qErr == nil {
					if rows.Next() {
						slog.Warn("sqlite: foreign_key_check reported violations after migration", "version", v+1)
					}
					rows.Close()
				}
				if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
					return fmt.Errorf("re-enable FK after v%d: %w", v, err)
				}
			}
			// Post-SQL backfill hooks for migrations needing app-side logic.
			// modernc.org/sqlite lacks regexp_replace, so the v15 → v16
			// basename columns must be populated via a Go loop.
			if v == 15 {
				if err := backfillV16(context.Background(), db); err != nil {
					return fmt.Errorf("v16 backfill: %w", err)
				}
			}
			slog.Info("sqlite: applied migration", "version", v+1)
		}
	}

	return seedMasterTenant(db)
}

// seedMasterTenant ensures the master tenant row exists (idempotent).
func seedMasterTenant(db *sql.DB) error {
	_, err := db.Exec(
		`INSERT OR IGNORE INTO tenants (id, name, slug, status) VALUES (?, 'Master', 'master', 'active')`,
		"0193a5b0-7000-7000-8000-000000000001",
	)
	if err != nil {
		slog.Warn("sqlite: seed master tenant failed", "error", err)
	}
	return nil
}
