-- GoClaw Tenant Database Schema
-- Auto-generated from PG migrations
-- Excludes master-only tables

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- From 000001_init_schema.up.sql
-- GoClaw Multi-Tenant Schema
-- Requires: pgcrypto, pgvector extensions

CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "vector";

-- UUID v7 function (matching backend-go)
CREATE OR REPLACE FUNCTION uuid_generate_v7() RETURNS uuid AS $$
DECLARE
    unix_ts_ms bytea;
    uuid_bytes bytea;
BEGIN
    unix_ts_ms = substring(int8send(floor(extract(epoch from clock_timestamp()) * 1000)::bigint) from 3);
    uuid_bytes = unix_ts_ms || gen_random_bytes(10);
    uuid_bytes = set_byte(uuid_bytes, 6, (b'0111' || get_byte(uuid_bytes, 6)::bit(4))::bit(8)::int);
    uuid_bytes = set_byte(uuid_bytes, 8, (b'10' || get_byte(uuid_bytes, 8)::bit(6))::bit(8)::int);
    RETURN encode(uuid_bytes, 'hex')::uuid;
END
$$ LANGUAGE plpgsql VOLATILE;

-- ============================================================
-- 1. LLM Providers
-- ============================================================

CREATE TABLE llm_providers (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    name          VARCHAR(50) NOT NULL UNIQUE,
    display_name  VARCHAR(255),
    provider_type VARCHAR(30) NOT NULL DEFAULT 'openai_compat',
    api_base      TEXT,
    api_key       TEXT,
    enabled       BOOLEAN NOT NULL DEFAULT true,
    settings      JSONB NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ DEFAULT NOW(),
    updated_at    TIMESTAMPTZ DEFAULT NOW()
);

-- ============================================================
-- 2. Agents & Access Control
-- ============================================================

CREATE TABLE agents (
    id                    UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_key             VARCHAR(100) NOT NULL UNIQUE,
    display_name          VARCHAR(255),
    owner_id              VARCHAR(255) NOT NULL,
    provider              VARCHAR(50) NOT NULL DEFAULT 'openrouter',
    model                 VARCHAR(200) NOT NULL,
    context_window        INT NOT NULL DEFAULT 200000,
    max_tool_iterations   INT NOT NULL DEFAULT 20,
    workspace             TEXT NOT NULL DEFAULT '.',
    restrict_to_workspace BOOLEAN NOT NULL DEFAULT true,
    tools_config          JSONB NOT NULL DEFAULT '{}',
    sandbox_config        JSONB,
    subagents_config      JSONB,
    memory_config         JSONB,
    compaction_config     JSONB,
    context_pruning       JSONB,
    other_config          JSONB NOT NULL DEFAULT '{}',
    is_default            BOOLEAN NOT NULL DEFAULT false,
    agent_type            VARCHAR(20) NOT NULL DEFAULT 'open',
    status                VARCHAR(20) DEFAULT 'active',
    created_at            TIMESTAMPTZ DEFAULT NOW(),
    updated_at            TIMESTAMPTZ DEFAULT NOW(),
    deleted_at            TIMESTAMPTZ
);

CREATE INDEX idx_agents_owner ON agents(owner_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_agents_status ON agents(status) WHERE deleted_at IS NULL;

CREATE TABLE agent_shares (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id    VARCHAR(255) NOT NULL,
    role       VARCHAR(20) NOT NULL DEFAULT 'user',
    granted_by VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(agent_id, user_id)
);

CREATE INDEX idx_agent_shares_user ON agent_shares(user_id);

-- ============================================================
-- 3. Context Files & User Profiles
-- ============================================================

CREATE TABLE agent_context_files (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    file_name  VARCHAR(255) NOT NULL,
    content    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(agent_id, file_name)
);

CREATE TABLE user_context_files (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id    VARCHAR(255) NOT NULL,
    file_name  VARCHAR(255) NOT NULL,
    content    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(agent_id, user_id, file_name)
);

CREATE TABLE user_agent_profiles (
    agent_id      UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id       VARCHAR(255) NOT NULL,
    workspace     TEXT,
    first_seen_at TIMESTAMPTZ DEFAULT NOW(),
    last_seen_at  TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (agent_id, user_id)
);

CREATE TABLE user_agent_overrides (
    id       UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id  VARCHAR(255) NOT NULL,
    provider VARCHAR(50),
    model    VARCHAR(200),
    settings JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(agent_id, user_id)
);

-- ============================================================
-- 4. Sessions
-- ============================================================

CREATE TABLE sessions (
    id                            UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    session_key                   VARCHAR(500) NOT NULL UNIQUE,
    agent_id                      UUID REFERENCES agents(id),
    user_id                       VARCHAR(255),
    messages                      JSONB NOT NULL DEFAULT '[]',
    summary                       TEXT,
    model                         VARCHAR(200),
    provider                      VARCHAR(50),
    channel                       VARCHAR(50),
    input_tokens                  BIGINT NOT NULL DEFAULT 0,
    output_tokens                 BIGINT NOT NULL DEFAULT 0,
    compaction_count              INT NOT NULL DEFAULT 0,
    memory_flush_compaction_count INT NOT NULL DEFAULT 0,
    memory_flush_at               BIGINT DEFAULT 0,
    label                         VARCHAR(500),
    spawned_by                    VARCHAR(200),
    spawn_depth                   INT NOT NULL DEFAULT 0,
    created_at                    TIMESTAMPTZ DEFAULT NOW(),
    updated_at                    TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_sessions_agent ON sessions(agent_id);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_updated ON sessions(updated_at DESC);

-- ============================================================
-- 5. Memory (pgvector + tsvector)
-- ============================================================

CREATE TABLE memory_documents (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id    VARCHAR(255),
    path       VARCHAR(500) NOT NULL,
    content    TEXT NOT NULL DEFAULT '',
    hash       VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_memdoc_unique ON memory_documents(agent_id, COALESCE(user_id, ''), path);
CREATE INDEX idx_memdoc_agent_user ON memory_documents(agent_id, user_id);

CREATE TABLE memory_chunks (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    document_id UUID REFERENCES memory_documents(id) ON DELETE CASCADE,
    user_id     VARCHAR(255),
    path        TEXT NOT NULL,
    start_line  INT NOT NULL DEFAULT 0,
    end_line    INT NOT NULL DEFAULT 0,
    hash        VARCHAR(64) NOT NULL,
    text        TEXT NOT NULL,
    embedding   vector(1536),
    tsv         tsvector GENERATED ALWAYS AS (to_tsvector('simple', text)) STORED,
    -- NOTE: 'simple' config (no stemming) works correctly for Vietnamese and other non-English languages
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    updated_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_mem_agent_user ON memory_chunks(agent_id, user_id);
CREATE INDEX idx_mem_global ON memory_chunks(agent_id) WHERE user_id IS NULL;
CREATE INDEX idx_mem_document ON memory_chunks(document_id);
CREATE INDEX idx_mem_tsv ON memory_chunks USING GIN(tsv);
CREATE INDEX idx_mem_vec ON memory_chunks USING hnsw(embedding vector_cosine_ops);

CREATE TABLE embedding_cache (
    hash      VARCHAR(64) NOT NULL,
    provider  VARCHAR(50) NOT NULL,
    model     VARCHAR(200) NOT NULL,
    embedding vector(1536),
    dims      INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (hash, provider, model)
);

-- ============================================================
-- 6. Skills (metadata + filesystem content)
-- ============================================================

CREATE TABLE skills (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    name         VARCHAR(255) NOT NULL,
    slug         VARCHAR(255) NOT NULL UNIQUE,
    description  TEXT,
    owner_id     VARCHAR(255) NOT NULL,
    visibility   VARCHAR(10) NOT NULL DEFAULT 'private',
    version      INT NOT NULL DEFAULT 1,
    status       VARCHAR(20) NOT NULL DEFAULT 'active',
    frontmatter  JSONB NOT NULL DEFAULT '{}',
    file_path    TEXT NOT NULL,
    file_size    BIGINT NOT NULL DEFAULT 0,
    file_hash    VARCHAR(64),
    embedding    vector(1536),
    tags         TEXT[],
    created_at   TIMESTAMPTZ DEFAULT NOW(),
    updated_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_skills_owner ON skills(owner_id);
CREATE INDEX idx_skills_visibility ON skills(visibility) WHERE status = 'active';
CREATE INDEX idx_skills_slug ON skills(slug);
CREATE INDEX idx_skills_embedding ON skills USING hnsw(embedding vector_cosine_ops);
CREATE INDEX idx_skills_tags ON skills USING GIN(tags);

CREATE TABLE skill_agent_grants (
    id             UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    skill_id       UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    agent_id       UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    pinned_version INT NOT NULL,
    granted_by     VARCHAR(255) NOT NULL,
    created_at     TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(skill_id, agent_id)
);

CREATE INDEX idx_skill_agent_grants_agent ON skill_agent_grants(agent_id);

CREATE TABLE skill_user_grants (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    skill_id   UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    user_id    VARCHAR(255) NOT NULL,
    granted_by VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(skill_id, user_id)
);

CREATE INDEX idx_skill_user_grants_user ON skill_user_grants(user_id);

-- ============================================================
-- 7. Cron Jobs
-- ============================================================

CREATE TABLE cron_jobs (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_id         UUID REFERENCES agents(id),
    user_id          TEXT,
    name             VARCHAR(255) NOT NULL,
    enabled          BOOLEAN NOT NULL DEFAULT true,
    schedule_kind    VARCHAR(10) NOT NULL,
    cron_expression  VARCHAR(100),
    interval_ms      BIGINT,
    run_at           TIMESTAMPTZ,
    timezone         VARCHAR(50),
    payload          JSONB NOT NULL,
    delete_after_run BOOLEAN NOT NULL DEFAULT false,
    next_run_at      TIMESTAMPTZ,
    last_run_at      TIMESTAMPTZ,
    last_status      VARCHAR(20),
    last_error       TEXT,
    created_at       TIMESTAMPTZ DEFAULT NOW(),
    updated_at       TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_cron_jobs_user_id ON cron_jobs(user_id);
CREATE INDEX idx_cron_jobs_agent_user ON cron_jobs(agent_id, user_id);

CREATE TABLE cron_run_logs (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    job_id        UUID NOT NULL REFERENCES cron_jobs(id) ON DELETE CASCADE,
    agent_id      UUID REFERENCES agents(id),
    status        VARCHAR(20) NOT NULL,
    summary       TEXT,
    error         TEXT,
    duration_ms   INT,
    input_tokens  INT DEFAULT 0,
    output_tokens INT DEFAULT 0,
    ran_at        TIMESTAMPTZ DEFAULT NOW(),
    created_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_cron_run_logs_job ON cron_run_logs(job_id, ran_at DESC);

-- ============================================================
-- 8. Pairing
-- ============================================================

CREATE TABLE pairing_requests (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    code       VARCHAR(8) NOT NULL UNIQUE,
    sender_id  VARCHAR(200) NOT NULL,
    channel    VARCHAR(255) NOT NULL,
    chat_id    VARCHAR(200) NOT NULL,
    account_id VARCHAR(100) NOT NULL DEFAULT 'default',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE paired_devices (
    id        UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    sender_id VARCHAR(200) NOT NULL,
    channel   VARCHAR(255) NOT NULL,
    chat_id   VARCHAR(200) NOT NULL,
    paired_by VARCHAR(100) NOT NULL DEFAULT 'operator',
    paired_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(sender_id, channel)
);

-- ============================================================
-- 9. LLM Tracing
-- ============================================================

CREATE TABLE traces (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_id            UUID,
    user_id             VARCHAR(255),
    session_key         TEXT,
    run_id              TEXT,
    start_time          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    end_time            TIMESTAMPTZ,
    duration_ms         INT,
    name                TEXT,
    channel             VARCHAR(50),
    input_preview       TEXT,
    output_preview      TEXT,
    total_input_tokens  INT DEFAULT 0,
    total_output_tokens INT DEFAULT 0,
    total_cost          NUMERIC(12,6) DEFAULT 0,
    span_count          INT DEFAULT 0,
    llm_call_count      INT DEFAULT 0,
    tool_call_count     INT DEFAULT 0,
    status              VARCHAR(20) DEFAULT 'running',
    error               TEXT,
    metadata            JSONB,
    tags                TEXT[],
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_traces_agent_time ON traces(agent_id, created_at DESC);
CREATE INDEX idx_traces_user_time ON traces(user_id, created_at DESC) WHERE user_id IS NOT NULL;
CREATE INDEX idx_traces_session ON traces(session_key, created_at DESC) WHERE session_key IS NOT NULL;
CREATE INDEX idx_traces_status ON traces(status) WHERE status = 'error';

CREATE TABLE spans (
    id             UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    trace_id       UUID NOT NULL,
    parent_span_id UUID,
    agent_id       UUID,
    span_type      VARCHAR(20) NOT NULL,
    name           TEXT,
    start_time     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    end_time       TIMESTAMPTZ,
    duration_ms    INT,
    status         VARCHAR(20) DEFAULT 'running',
    error          TEXT,
    level          VARCHAR(10) DEFAULT 'DEFAULT',
    model          VARCHAR(200),
    provider       VARCHAR(50),
    input_tokens   INT,
    output_tokens  INT,
    total_cost     NUMERIC(12,8),
    finish_reason  VARCHAR(50),
    model_params   JSONB,
    tool_name      VARCHAR(200),
    tool_call_id   VARCHAR(100),
    input_preview  TEXT,
    output_preview TEXT,
    metadata       JSONB,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_spans_trace ON spans(trace_id, start_time);
CREATE INDEX idx_spans_parent ON spans(parent_span_id) WHERE parent_span_id IS NOT NULL;
CREATE INDEX idx_spans_agent_time ON spans(agent_id, created_at DESC);
CREATE INDEX idx_spans_type ON spans(span_type, created_at DESC);
CREATE INDEX idx_spans_model ON spans(model, created_at DESC) WHERE model IS NOT NULL;
CREATE INDEX idx_spans_error ON spans(status) WHERE status = 'error';

-- ============================================================
-- 10. MCP Servers (External Tool Providers)
-- ============================================================

CREATE TABLE mcp_servers (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    name          VARCHAR(255) NOT NULL UNIQUE,
    display_name  VARCHAR(255),
    transport     VARCHAR(50) NOT NULL,            -- "stdio", "sse", "streamable-http"
    command       TEXT,                             -- stdio: command to spawn
    args          JSONB DEFAULT '[]',               -- stdio: command arguments
    url           TEXT,                             -- sse/http: server URL
    headers       JSONB DEFAULT '{}',               -- sse/http: HTTP headers
    env           JSONB DEFAULT '{}',               -- stdio: environment variables
    api_key       TEXT,                             -- encrypted (AES-256-GCM)
    tool_prefix   VARCHAR(50),                      -- optional prefix for tool names
    timeout_sec   INT DEFAULT 60,
    settings      JSONB NOT NULL DEFAULT '{}',
    enabled       BOOLEAN NOT NULL DEFAULT true,
    created_by    VARCHAR(255) NOT NULL,
    created_at    TIMESTAMPTZ DEFAULT NOW(),
    updated_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE mcp_agent_grants (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    server_id        UUID NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    agent_id         UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    enabled          BOOLEAN NOT NULL DEFAULT true,
    tool_allow       JSONB,                         -- ["tool1", "tool2"] (null = all)
    tool_deny        JSONB,                         -- ["dangerous_tool"]
    config_overrides JSONB,
    granted_by       VARCHAR(255) NOT NULL,
    created_at       TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(server_id, agent_id)
);

CREATE INDEX idx_mcp_agent_grants_agent ON mcp_agent_grants(agent_id);

CREATE TABLE mcp_user_grants (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    server_id        UUID NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    user_id          VARCHAR(255) NOT NULL,
    enabled          BOOLEAN NOT NULL DEFAULT true,
    tool_allow       JSONB,
    tool_deny        JSONB,
    granted_by       VARCHAR(255) NOT NULL,
    created_at       TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(server_id, user_id)
);

CREATE INDEX idx_mcp_user_grants_user ON mcp_user_grants(user_id);

CREATE TABLE mcp_access_requests (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    server_id     UUID NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    agent_id      UUID REFERENCES agents(id) ON DELETE CASCADE,
    user_id       VARCHAR(255),
    scope         VARCHAR(10) NOT NULL,             -- "agent" or "user"
    status        VARCHAR(20) NOT NULL DEFAULT 'pending', -- "pending", "approved", "rejected"
    reason        TEXT,
    tool_allow    JSONB,                            -- requested tool subset (null = all)
    requested_by  VARCHAR(255) NOT NULL,
    reviewed_by   VARCHAR(255),
    reviewed_at   TIMESTAMPTZ,
    review_note   TEXT,
    created_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_mcp_requests_status ON mcp_access_requests(status) WHERE status = 'pending';
CREATE INDEX idx_mcp_requests_server ON mcp_access_requests(server_id);

-- ============================================================
-- 11. Custom Tools (Dynamic Tools from DB)
-- ============================================================

CREATE TABLE custom_tools (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    name            VARCHAR(100) NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    parameters      JSONB NOT NULL DEFAULT '{}',
    command         TEXT NOT NULL,
    working_dir     TEXT DEFAULT '',
    timeout_seconds INT DEFAULT 60,
    env             BYTEA,                               -- encrypted env vars (AES-256-GCM)
    agent_id        UUID REFERENCES agents(id) ON DELETE CASCADE,
    enabled         BOOLEAN DEFAULT TRUE,
    created_by      VARCHAR(255) NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

-- Global tools: unique name when agent_id IS NULL
CREATE UNIQUE INDEX idx_custom_tools_name_global ON custom_tools(name) WHERE agent_id IS NULL;
-- Per-agent tools: unique (name, agent_id) when agent_id IS NOT NULL
CREATE UNIQUE INDEX idx_custom_tools_name_agent ON custom_tools(name, agent_id) WHERE agent_id IS NOT NULL;
-- Fast lookup by agent
CREATE INDEX idx_custom_tools_agent ON custom_tools(agent_id) WHERE agent_id IS NOT NULL;

-- ============================================================
-- 12. Channel Instances
-- ============================================================

CREATE TABLE channel_instances (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    name            VARCHAR(100) NOT NULL UNIQUE,
    display_name    VARCHAR(255) DEFAULT '',
    channel_type    VARCHAR(50) NOT NULL,
    agent_id        UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    credentials     BYTEA,
    config          JSONB DEFAULT '{}',
    enabled         BOOLEAN DEFAULT true,
    created_by      VARCHAR(255) DEFAULT '',
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_channel_instances_type ON channel_instances(channel_type);
CREATE INDEX idx_channel_instances_agent ON channel_instances(agent_id);

-- ============================================================
-- 13. Config Secrets
-- ============================================================

CREATE TABLE config_secrets (
    key         VARCHAR(100) PRIMARY KEY,
    value       BYTEA NOT NULL,
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    updated_at  TIMESTAMPTZ DEFAULT NOW()
);

-- ============================================================
-- 14. Group File Writers (Telegram group write permissions)
-- ============================================================

CREATE TABLE group_file_writers (
    agent_id     UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    group_id     VARCHAR(255) NOT NULL,
    user_id      VARCHAR(255) NOT NULL,
    display_name VARCHAR(255),
    username     VARCHAR(255),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (agent_id, group_id, user_id)
);

-- From 000002_agent_links.up.sql
-- ============================================================
-- Agent frontmatter (short expertise/capability summary for delegation + UI display).
-- NOTE: This is NOT the same as other_config.description which is the
-- LLM summoning prompt used to initialize the agent's identity.
-- ============================================================

ALTER TABLE agents ADD COLUMN IF NOT EXISTS frontmatter TEXT;

-- Search support: FTS (tsvector) + semantic (pgvector) for agent discovery.
-- Pattern follows skills table: hybrid BM25 + cosine similarity.
-- tsv is auto-generated from display_name + frontmatter.
-- embedding is populated on create/update when an embedding provider is available.
ALTER TABLE agents ADD COLUMN IF NOT EXISTS tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', COALESCE(display_name, '') || ' ' || COALESCE(frontmatter, ''))) STORED;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS embedding vector(1536);
CREATE INDEX IF NOT EXISTS idx_agents_tsv ON agents USING GIN(tsv);
CREATE INDEX IF NOT EXISTS idx_agents_embedding ON agents USING hnsw(embedding vector_cosine_ops);

-- ============================================================
-- Agent Links (inter-agent delegation permissions)
-- ============================================================

CREATE TABLE agent_links (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    source_agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    target_agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    direction       VARCHAR(20) NOT NULL DEFAULT 'outbound',
    description     TEXT,
    max_concurrent  INT NOT NULL DEFAULT 3,
    settings        JSONB NOT NULL DEFAULT '{}',
    status          VARCHAR(20) NOT NULL DEFAULT 'active',
    created_by      VARCHAR(255) NOT NULL,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(source_agent_id, target_agent_id),
    CHECK (source_agent_id != target_agent_id)
);

CREATE INDEX idx_agent_links_source ON agent_links(source_agent_id) WHERE status = 'active';
CREATE INDEX idx_agent_links_target ON agent_links(target_agent_id) WHERE status = 'active';

-- ============================================================
-- Linked traces for delegation: parent_trace_id on traces table
-- allows navigating between caller and delegate traces.
-- ============================================================

ALTER TABLE traces ADD COLUMN IF NOT EXISTS parent_trace_id UUID;
CREATE INDEX IF NOT EXISTS idx_traces_parent ON traces(parent_trace_id) WHERE parent_trace_id IS NOT NULL;

-- From 000003_agent_teams.up.sql
-- ============================================================
-- Agent Teams (collaborative multi-agent coordination)
-- ============================================================

CREATE TABLE agent_teams (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    name          VARCHAR(255) NOT NULL,
    lead_agent_id UUID NOT NULL REFERENCES agents(id),
    description   TEXT,
    status        VARCHAR(20) NOT NULL DEFAULT 'active',
    settings      JSONB NOT NULL DEFAULT '{}',
    created_by    VARCHAR(255) NOT NULL,
    created_at    TIMESTAMPTZ DEFAULT NOW(),
    updated_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE agent_team_members (
    team_id   UUID NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
    agent_id  UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    role      VARCHAR(20) NOT NULL DEFAULT 'member',
    joined_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (team_id, agent_id)
);

-- ============================================================
-- Team Tasks (shared task list with self-claim + dependencies)
-- ============================================================

CREATE TABLE team_tasks (
    id             UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    team_id        UUID NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
    subject        VARCHAR(500) NOT NULL,
    description    TEXT,
    status         VARCHAR(20) NOT NULL DEFAULT 'pending',
    owner_agent_id UUID REFERENCES agents(id),
    blocked_by     UUID[],
    priority       INT NOT NULL DEFAULT 0,
    result         TEXT,
    created_at     TIMESTAMPTZ DEFAULT NOW(),
    updated_at     TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_team_tasks_team ON team_tasks(team_id);
CREATE INDEX idx_team_tasks_status ON team_tasks(team_id, status);

-- ============================================================
-- Team Messages (peer-to-peer mailbox)
-- ============================================================

CREATE TABLE team_messages (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    team_id       UUID NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
    from_agent_id UUID NOT NULL REFERENCES agents(id),
    to_agent_id   UUID REFERENCES agents(id),
    content       TEXT NOT NULL,
    message_type  VARCHAR(30) NOT NULL DEFAULT 'chat',
    read          BOOLEAN NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_team_messages_to ON team_messages(team_id, to_agent_id, read);

-- ============================================================
-- Link agent_links to teams: team-created links have team_id set.
-- ON DELETE SET NULL → when team is deleted, links become manual.
-- ============================================================

ALTER TABLE agent_links ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;

-- From 000004_teams_v2.up.sql
-- 000004_teams_v2: FTS for team_tasks + delegation_history table

-- 1. Full-text search on team_tasks (subject + description)
ALTER TABLE team_tasks ADD COLUMN IF NOT EXISTS tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', subject || ' ' || COALESCE(description, ''))) STORED;
CREATE INDEX IF NOT EXISTS idx_team_tasks_tsv ON team_tasks USING GIN(tsv);

-- 2. Delegation history (persisted record of every delegation)
CREATE TABLE IF NOT EXISTS delegation_history (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    source_agent_id UUID NOT NULL REFERENCES agents(id),
    target_agent_id UUID NOT NULL REFERENCES agents(id),
    team_id         UUID REFERENCES agent_teams(id) ON DELETE SET NULL,
    team_task_id    UUID REFERENCES team_tasks(id) ON DELETE SET NULL,
    user_id         VARCHAR(255),
    task            TEXT NOT NULL,
    mode            VARCHAR(10) NOT NULL DEFAULT 'sync',
    status          VARCHAR(20) NOT NULL DEFAULT 'completed',
    result          TEXT,
    error           TEXT,
    iterations      INT DEFAULT 0,
    trace_id        UUID,
    duration_ms     INT,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    completed_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_delegation_history_source ON delegation_history(source_agent_id);
CREATE INDEX IF NOT EXISTS idx_delegation_history_team ON delegation_history(team_id);
CREATE INDEX IF NOT EXISTS idx_delegation_history_created ON delegation_history(created_at DESC);

-- From 000005_phase4.up.sql
-- 000005_phase4: Agent handoff routing table

CREATE TABLE IF NOT EXISTS handoff_routes (
    id             UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    channel        VARCHAR(50) NOT NULL,
    chat_id        VARCHAR(255) NOT NULL,
    from_agent_key VARCHAR(255) NOT NULL,
    to_agent_key   VARCHAR(255) NOT NULL,
    reason         TEXT,
    created_by     VARCHAR(255),
    created_at     TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(channel, chat_id)
);

-- From 000006_builtin_tools.up.sql
CREATE TABLE IF NOT EXISTS builtin_tools (
    name            VARCHAR(100) PRIMARY KEY,
    display_name    VARCHAR(255) NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    category        VARCHAR(50) NOT NULL DEFAULT 'general',
    enabled         BOOLEAN NOT NULL DEFAULT true,
    settings        JSONB NOT NULL DEFAULT '{}',
    requires        TEXT[] DEFAULT '{}',
    metadata        JSONB DEFAULT '{}',
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_builtin_tools_category ON builtin_tools(category);

-- Add metadata column to custom_tools for future extensibility
ALTER TABLE custom_tools ADD COLUMN IF NOT EXISTS metadata JSONB DEFAULT '{}';

-- From 000007_team_metadata.up.sql
-- Add task_id to team_messages for linking messages to tasks
ALTER TABLE team_messages ADD COLUMN IF NOT EXISTS task_id UUID REFERENCES team_tasks(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_team_messages_task ON team_messages(task_id) WHERE task_id IS NOT NULL;

-- Add metadata JSONB to all team-related tables
ALTER TABLE team_messages ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}';
ALTER TABLE team_tasks ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}';
ALTER TABLE delegation_history ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}';
ALTER TABLE handoff_routes ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}';

-- From 000008_team_tasks_user_scope.up.sql
ALTER TABLE team_tasks ADD COLUMN user_id VARCHAR(255);
ALTER TABLE team_tasks ADD COLUMN channel VARCHAR(50);
CREATE INDEX idx_team_tasks_user_scope ON team_tasks(team_id, user_id) WHERE user_id IS NOT NULL;

-- From 000009_add_quota_index.up.sql
-- Partial index for quota checker: efficiently counts top-level traces per user in time windows.
-- Eliminates parent_trace_id IS NULL post-filter (89% of traces are top-level).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_traces_quota
ON traces (user_id, created_at DESC)
WHERE parent_trace_id IS NULL AND user_id IS NOT NULL;

-- From 000010_agents_md_v2.up.sql
-- Force-update ALL existing AGENTS.md to v2 (managed mode optimized, ~50% smaller)
-- Affects both agent-level (predefined) and per-user (open) context files.

UPDATE agent_context_files
SET content = $AGENTS_V2$# AGENTS.md - How You Operate

## Identity & Context

Your identity is in SOUL.md. Your user's profile is in USER.md. Both are loaded above — embody them, don't re-read them.

For open agents: you can edit SOUL.md, USER.md, and AGENTS.md with `write_file` or `edit` to customize yourself over time.

## Conversational Style

Talk like a person, not a customer service bot.

- **Don't parrot** — never repeat the user's question back to them before answering.
- **Don't pad** — no "Great question!", "Certainly!", "I'd be happy to help!" Just help.
- **Don't always close with offers** — "Bạn cần gì thêm không?" after every message is robotic. Only ask when genuinely relevant.
- **Answer first** — lead with the answer, explain after if needed.
- **Short is fine** — "OK xong rồi" is a valid response. Not everything needs a paragraph.
- **Match their energy** — casual user → casual reply. Short question → short answer.
- **Vary your format** — not everything needs bullet points or numbered lists. Sometimes a sentence is enough.

## Memory

You start fresh each session. Use tools to maintain continuity:

- **Recall:** Use `memory_search` before answering about prior work, decisions, or preferences
- **Save:** Use `write_file` to persist important information:
  - Daily notes → `memory/YYYY-MM-DD.md` (raw logs, what happened today)
  - Long-term → `MEMORY.md` (curated: key decisions, lessons, significant events)
- **No "mental notes"** — if you want to remember something, write it to a file NOW with a tool call
- When asked to "remember this" → write immediately, don't just acknowledge

### MEMORY.md Privacy

- Only reference MEMORY.md content in **private/direct chats** with your user
- In group chats or shared sessions, do NOT surface personal memory content

## Group Chats

### Know When to Speak

**Respond when:**

- Directly mentioned or asked a question
- You can add genuine value (info, insight, help)
- Something witty/funny fits naturally
- Correcting important misinformation

**Stay silent (NO_REPLY) when:**

- Just casual banter between humans
- Someone already answered the question
- Your response would just be "yeah" or "nice"
- The conversation flows fine without you
- Adding a message would interrupt the vibe

**The rule:** Humans don't respond to every message. Neither should you. Quality > quantity.

**Avoid the triple-tap:** Don't respond multiple times to the same message. One thoughtful response beats three fragments.

### React Like a Human

On platforms with reactions (Discord, Slack), use emoji reactions naturally:

- Appreciate something but don't need to reply → 👍 ❤️ 🙌
- Something funny → 😂 💀
- Interesting or thought-provoking → 🤔 💡
- Acknowledge without interrupting → 👀 ✅

One reaction per message max.

## Platform Formatting

- **Discord/WhatsApp:** No markdown tables — use bullet lists instead
- **Discord links:** Wrap in `<>` to suppress embeds: `<https://example.com>`
- **WhatsApp:** No headers — use **bold** or CAPS for emphasis

## Scheduling

Use the `cron` tool for periodic or timed tasks. Examples:

```
cron(action="add", job={ name: "morning-briefing", schedule: { kind: "cron", expr: "0 9 * * 1-5" }, message: "Morning briefing: calendar today, pending tasks, urgent items." })
cron(action="add", job={ name: "memory-review", schedule: { kind: "cron", expr: "0 22 * * 0" }, message: "Review recent memory files. Update MEMORY.md with significant learnings." })
```

Tips:

- Keep messages specific and actionable
- Use `kind: "at"` for one-shot reminders (auto-deletes after running)
- Use `deliver: true` with `channel` and `to` to send output to a chat
- Don't create too many frequent jobs — batch related checks

## Voice

If you have TTS capability, use voice for stories and "storytime" moments — more engaging than walls of text.
$AGENTS_V2$,
    updated_at = NOW()
WHERE file_name = 'AGENTS.md';

UPDATE user_context_files
SET content = $AGENTS_V2$# AGENTS.md - How You Operate

## Identity & Context

Your identity is in SOUL.md. Your user's profile is in USER.md. Both are loaded above — embody them, don't re-read them.

For open agents: you can edit SOUL.md, USER.md, and AGENTS.md with `write_file` or `edit` to customize yourself over time.

## Conversational Style

Talk like a person, not a customer service bot.

- **Don't parrot** — never repeat the user's question back to them before answering.
- **Don't pad** — no "Great question!", "Certainly!", "I'd be happy to help!" Just help.
- **Don't always close with offers** — "Bạn cần gì thêm không?" after every message is robotic. Only ask when genuinely relevant.
- **Answer first** — lead with the answer, explain after if needed.
- **Short is fine** — "OK xong rồi" is a valid response. Not everything needs a paragraph.
- **Match their energy** — casual user → casual reply. Short question → short answer.
- **Vary your format** — not everything needs bullet points or numbered lists. Sometimes a sentence is enough.

## Memory

You start fresh each session. Use tools to maintain continuity:

- **Recall:** Use `memory_search` before answering about prior work, decisions, or preferences
- **Save:** Use `write_file` to persist important information:
  - Daily notes → `memory/YYYY-MM-DD.md` (raw logs, what happened today)
  - Long-term → `MEMORY.md` (curated: key decisions, lessons, significant events)
- **No "mental notes"** — if you want to remember something, write it to a file NOW with a tool call
- When asked to "remember this" → write immediately, don't just acknowledge

### MEMORY.md Privacy

- Only reference MEMORY.md content in **private/direct chats** with your user
- In group chats or shared sessions, do NOT surface personal memory content

## Group Chats

### Know When to Speak

**Respond when:**

- Directly mentioned or asked a question
- You can add genuine value (info, insight, help)
- Something witty/funny fits naturally
- Correcting important misinformation

**Stay silent (NO_REPLY) when:**

- Just casual banter between humans
- Someone already answered the question
- Your response would just be "yeah" or "nice"
- The conversation flows fine without you
- Adding a message would interrupt the vibe

**The rule:** Humans don't respond to every message. Neither should you. Quality > quantity.

**Avoid the triple-tap:** Don't respond multiple times to the same message. One thoughtful response beats three fragments.

### React Like a Human

On platforms with reactions (Discord, Slack), use emoji reactions naturally:

- Appreciate something but don't need to reply → 👍 ❤️ 🙌
- Something funny → 😂 💀
- Interesting or thought-provoking → 🤔 💡
- Acknowledge without interrupting → 👀 ✅

One reaction per message max.

## Platform Formatting

- **Discord/WhatsApp:** No markdown tables — use bullet lists instead
- **Discord links:** Wrap in `<>` to suppress embeds: `<https://example.com>`
- **WhatsApp:** No headers — use **bold** or CAPS for emphasis

## Scheduling

Use the `cron` tool for periodic or timed tasks. Examples:

```
cron(action="add", job={ name: "morning-briefing", schedule: { kind: "cron", expr: "0 9 * * 1-5" }, message: "Morning briefing: calendar today, pending tasks, urgent items." })
cron(action="add", job={ name: "memory-review", schedule: { kind: "cron", expr: "0 22 * * 0" }, message: "Review recent memory files. Update MEMORY.md with significant learnings." })
```

Tips:

- Keep messages specific and actionable
- Use `kind: "at"` for one-shot reminders (auto-deletes after running)
- Use `deliver: true` with `channel` and `to` to send output to a chat
- Don't create too many frequent jobs — batch related checks

## Voice

If you have TTS capability, use voice for stories and "storytime" moments — more engaging than walls of text.
$AGENTS_V2$,
    updated_at = NOW()
WHERE file_name = 'AGENTS.md';

-- From 000011_session_profile_metadata.up.sql
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS metadata JSONB DEFAULT '{}';
ALTER TABLE user_agent_profiles ADD COLUMN IF NOT EXISTS metadata JSONB DEFAULT '{}';
ALTER TABLE pairing_requests ADD COLUMN IF NOT EXISTS metadata JSONB DEFAULT '{}';
ALTER TABLE paired_devices ADD COLUMN IF NOT EXISTS metadata JSONB DEFAULT '{}';

-- From 000012_channel_pending_messages.up.sql
-- Channel pending messages: persists group chat messages when bot is NOT mentioned.
-- Used to provide conversational context when bot IS mentioned.
-- Supports LLM-based compaction (is_summary rows) and 7-day TTL cleanup.

CREATE TABLE channel_pending_messages (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    channel_name    VARCHAR(100) NOT NULL,
    history_key     VARCHAR(200) NOT NULL,
    sender          VARCHAR(255) NOT NULL,
    sender_id       VARCHAR(255) NOT NULL DEFAULT '',
    body            TEXT NOT NULL,
    platform_msg_id VARCHAR(100) NOT NULL DEFAULT '',
    is_summary      BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_channel_pending_messages_lookup
    ON channel_pending_messages (channel_name, history_key, created_at);

-- From 000013_knowledge_graph.up.sql
CREATE TABLE kg_entities (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id     VARCHAR(255) NOT NULL DEFAULT '',
    external_id VARCHAR(255) NOT NULL,
    name        TEXT NOT NULL,
    entity_type VARCHAR(100) NOT NULL,
    description TEXT DEFAULT '',
    properties  JSONB DEFAULT '{}',
    source_id   VARCHAR(255) DEFAULT '',
    confidence  FLOAT NOT NULL DEFAULT 1.0,
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    updated_at  TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(agent_id, user_id, external_id)
);

CREATE TABLE kg_relations (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id         UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id          VARCHAR(255) NOT NULL DEFAULT '',
    source_entity_id UUID NOT NULL REFERENCES kg_entities(id) ON DELETE CASCADE,
    relation_type    VARCHAR(200) NOT NULL,
    target_entity_id UUID NOT NULL REFERENCES kg_entities(id) ON DELETE CASCADE,
    confidence       FLOAT NOT NULL DEFAULT 1.0,
    properties       JSONB DEFAULT '{}',
    created_at       TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(agent_id, user_id, source_entity_id, relation_type, target_entity_id)
);

CREATE INDEX idx_kg_entities_scope ON kg_entities(agent_id, user_id);
CREATE INDEX idx_kg_entities_type ON kg_entities(agent_id, user_id, entity_type);
CREATE INDEX idx_kg_relations_source ON kg_relations(source_entity_id, relation_type);
CREATE INDEX idx_kg_relations_target ON kg_relations(target_entity_id);

-- From 000014_channel_contacts.up.sql
-- Channel contacts: auto-collected user info from all channels.
-- Global (not per-agent). Used for contact selector, future RBAC, analytics.
CREATE TABLE IF NOT EXISTS channel_contacts (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_type     VARCHAR(50) NOT NULL,
    channel_instance VARCHAR(255),
    sender_id        VARCHAR(255) NOT NULL,
    user_id          VARCHAR(255),
    display_name     VARCHAR(255),
    username         VARCHAR(255),
    avatar_url       TEXT,
    peer_kind        VARCHAR(20),
    metadata         JSONB DEFAULT '{}',
    merged_id        UUID,
    first_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (channel_type, sender_id)
);

CREATE INDEX IF NOT EXISTS idx_channel_contacts_instance ON channel_contacts(channel_instance) WHERE channel_instance IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_channel_contacts_merged ON channel_contacts(merged_id) WHERE merged_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_channel_contacts_search ON channel_contacts(display_name, username);

-- From 000015_agent_budget.up.sql
ALTER TABLE agents ADD COLUMN budget_monthly_cents INTEGER;

CREATE TABLE activity_logs (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    actor_type  VARCHAR(20) NOT NULL,
    actor_id    VARCHAR(255) NOT NULL,
    action      VARCHAR(100) NOT NULL,
    entity_type VARCHAR(50),
    entity_id   VARCHAR(255),
    details     JSONB,
    ip_address  VARCHAR(45),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_activity_logs_actor ON activity_logs (actor_type, actor_id);
CREATE INDEX idx_activity_logs_action ON activity_logs (action);
CREATE INDEX idx_activity_logs_entity ON activity_logs (entity_type, entity_id);
CREATE INDEX idx_activity_logs_created ON activity_logs (created_at DESC);

-- From 000016_usage_snapshots.up.sql
-- ============================================================
-- Part 1: New indexes on EXISTING tables (optimize aggregation)
-- ============================================================

-- Traces: snapshot worker scans by start_time for root traces only
-- Replaces Seq Scan (2.5ms→0.1ms at current scale, critical at 100K+ rows)
CREATE INDEX IF NOT EXISTS idx_traces_start_root ON traces (start_time DESC)
    WHERE parent_trace_id IS NULL;

-- Spans: snapshot worker joins on trace_id filtering by span_type
-- Current idx_spans_trace is (trace_id, start_time) — start_time useless here
-- This index lets PG filter span_type IN the index, avoiding wide-row fetches
CREATE INDEX IF NOT EXISTS idx_spans_trace_type ON spans (trace_id, span_type);

-- ============================================================
-- Part 2: usage_snapshots table
-- ============================================================

CREATE TABLE IF NOT EXISTS usage_snapshots (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    bucket_hour TIMESTAMPTZ NOT NULL,
    agent_id    UUID,
    provider    VARCHAR(50) NOT NULL DEFAULT '',
    model       VARCHAR(200) NOT NULL DEFAULT '',
    channel     VARCHAR(50) NOT NULL DEFAULT '',

    -- Token metrics
    input_tokens        BIGINT NOT NULL DEFAULT 0,
    output_tokens       BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens   BIGINT NOT NULL DEFAULT 0,
    cache_create_tokens BIGINT NOT NULL DEFAULT 0,
    thinking_tokens     BIGINT NOT NULL DEFAULT 0,

    -- Cost
    total_cost          NUMERIC(12,6) NOT NULL DEFAULT 0,

    -- Counts
    request_count       INTEGER NOT NULL DEFAULT 0,
    llm_call_count      INTEGER NOT NULL DEFAULT 0,
    tool_call_count     INTEGER NOT NULL DEFAULT 0,
    error_count         INTEGER NOT NULL DEFAULT 0,
    unique_users        INTEGER NOT NULL DEFAULT 0,

    -- Duration
    avg_duration_ms     INTEGER NOT NULL DEFAULT 0,

    -- Memory & Knowledge Graph (point-in-time counts)
    memory_docs         INTEGER NOT NULL DEFAULT 0,
    memory_chunks       INTEGER NOT NULL DEFAULT 0,
    kg_entities         INTEGER NOT NULL DEFAULT 0,
    kg_relations        INTEGER NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============================================================
-- Part 3: usage_snapshots indexes
-- ============================================================

-- Time-series queries: GROUP BY bucket_hour ORDER BY bucket_hour
CREATE INDEX IF NOT EXISTS idx_usage_snapshots_bucket ON usage_snapshots (bucket_hour DESC);

-- Agent-scoped time-series: WHERE agent_id = $1 ORDER BY bucket_hour
CREATE INDEX IF NOT EXISTS idx_usage_snapshots_agent_bucket ON usage_snapshots (agent_id, bucket_hour DESC);

-- Cross-filter: WHERE provider = $1 AND bucket_hour BETWEEN ...
CREATE INDEX IF NOT EXISTS idx_usage_snapshots_provider_bucket ON usage_snapshots (provider, bucket_hour DESC)
    WHERE provider != '';

-- Cross-filter: WHERE channel = $1 AND bucket_hour BETWEEN ...
CREATE INDEX IF NOT EXISTS idx_usage_snapshots_channel_bucket ON usage_snapshots (channel, bucket_hour DESC)
    WHERE channel != '';

-- Upsert dedup: ON CONFLICT — prevents duplicate snapshot rows
CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_snapshots_unique ON usage_snapshots (
    bucket_hour,
    COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'),
    provider, model, channel
);

-- From 000017_system_skills.up.sql
ALTER TABLE skills ADD COLUMN is_system BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE skills ADD COLUMN deps JSONB NOT NULL DEFAULT '{}';
ALTER TABLE skills ADD COLUMN enabled BOOLEAN NOT NULL DEFAULT true;
CREATE INDEX idx_skills_system ON skills(is_system) WHERE is_system = true;
CREATE INDEX idx_skills_enabled ON skills(enabled) WHERE enabled = false;

-- From 000018_team_tasks_workspace_followup.up.sql
-- ============================================================
-- Part 1: Team workspace (shared file storage)
-- ============================================================

-- Team workspace: shared file storage scoped by (team, chat_id).
-- chat_id stores the system-derived userID (stable across WS reconnects).
CREATE TABLE team_workspace_files (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    team_id     UUID NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
    channel     VARCHAR(50)  NOT NULL DEFAULT '',
    chat_id     VARCHAR(255) NOT NULL DEFAULT '',
    file_name   VARCHAR(255) NOT NULL,
    mime_type   VARCHAR(100),
    file_path   TEXT NOT NULL,
    size_bytes  BIGINT NOT NULL DEFAULT 0,
    uploaded_by UUID NOT NULL REFERENCES agents(id),
    task_id     UUID REFERENCES team_tasks(id) ON DELETE SET NULL,
    pinned      BOOLEAN NOT NULL DEFAULT false,
    tags        TEXT[] NOT NULL DEFAULT '{}',
    metadata    JSONB DEFAULT '{}',
    archived_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(team_id, chat_id, file_name)
);

CREATE INDEX idx_twf_team_scope ON team_workspace_files(team_id, chat_id);
CREATE INDEX idx_twf_uploaded_by  ON team_workspace_files(uploaded_by);
CREATE INDEX idx_twf_task         ON team_workspace_files(task_id) WHERE task_id IS NOT NULL;
CREATE INDEX idx_twf_archived     ON team_workspace_files(archived_at) WHERE archived_at IS NOT NULL;
CREATE INDEX idx_twf_pinned       ON team_workspace_files(team_id, pinned) WHERE pinned = true;
CREATE INDEX idx_twf_tags         ON team_workspace_files USING GIN(tags);

-- File version history.
CREATE TABLE team_workspace_file_versions (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    file_id     UUID NOT NULL REFERENCES team_workspace_files(id) ON DELETE CASCADE,
    version     INT NOT NULL,
    file_path   TEXT NOT NULL,
    size_bytes  BIGINT NOT NULL DEFAULT 0,
    uploaded_by UUID NOT NULL REFERENCES agents(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(file_id, version)
);

CREATE INDEX idx_twfv_file ON team_workspace_file_versions(file_id);

-- File comments / annotations.
CREATE TABLE team_workspace_comments (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    file_id     UUID NOT NULL REFERENCES team_workspace_files(id) ON DELETE CASCADE,
    agent_id    UUID NOT NULL REFERENCES agents(id),
    content     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_twfc_file ON team_workspace_comments(file_id);

-- ============================================================
-- Part 2: Team tasks v2 (locking, progress, audit, comments)
-- ============================================================

-- New columns on team_tasks
ALTER TABLE team_tasks ADD COLUMN task_type VARCHAR(30) NOT NULL DEFAULT 'general';
ALTER TABLE team_tasks ADD COLUMN task_number INT NOT NULL DEFAULT 0;
ALTER TABLE team_tasks ADD COLUMN identifier VARCHAR(20);
ALTER TABLE team_tasks ADD COLUMN created_by_agent_id UUID REFERENCES agents(id);
ALTER TABLE team_tasks ADD COLUMN assignee_user_id VARCHAR(255);
ALTER TABLE team_tasks ADD COLUMN parent_id UUID REFERENCES team_tasks(id) ON DELETE SET NULL;
ALTER TABLE team_tasks ADD COLUMN chat_id VARCHAR(255) DEFAULT '';
ALTER TABLE team_tasks ADD COLUMN locked_at TIMESTAMPTZ;
ALTER TABLE team_tasks ADD COLUMN lock_expires_at TIMESTAMPTZ;
ALTER TABLE team_tasks ADD COLUMN progress_percent INT DEFAULT 0 CHECK (progress_percent BETWEEN 0 AND 100);
ALTER TABLE team_tasks ADD COLUMN progress_step TEXT;

-- Indexes
CREATE INDEX idx_tt_parent ON team_tasks(parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX idx_tt_scope ON team_tasks(team_id, channel, chat_id);
CREATE INDEX idx_tt_type ON team_tasks(team_id, task_type);
CREATE INDEX idx_tt_lock ON team_tasks(lock_expires_at) WHERE lock_expires_at IS NOT NULL AND status = 'in_progress';
CREATE UNIQUE INDEX idx_tt_identifier ON team_tasks(team_id, identifier) WHERE identifier IS NOT NULL;

-- Task comments
CREATE TABLE team_task_comments (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    task_id    UUID NOT NULL REFERENCES team_tasks(id) ON DELETE CASCADE,
    agent_id   UUID REFERENCES agents(id),
    user_id    VARCHAR(255),
    content    TEXT NOT NULL,
    metadata   JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_ttc_task ON team_task_comments(task_id);

-- Audit history
CREATE TABLE team_task_events (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    task_id    UUID NOT NULL REFERENCES team_tasks(id) ON DELETE CASCADE,
    event_type VARCHAR(30) NOT NULL,
    actor_type VARCHAR(10) NOT NULL,
    actor_id   VARCHAR(255) NOT NULL,
    data       JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_tte_task ON team_task_events(task_id);

-- Task-workspace attachments
CREATE TABLE team_task_attachments (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    task_id    UUID NOT NULL REFERENCES team_tasks(id) ON DELETE CASCADE,
    file_id    UUID NOT NULL REFERENCES team_workspace_files(id) ON DELETE CASCADE,
    added_by   UUID REFERENCES agents(id),
    metadata   JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(task_id, file_id)
);
CREATE INDEX idx_tta_task ON team_task_attachments(task_id);

-- Backfill task_number (per-team sequential) and identifiers for existing tasks
DO $$
DECLARE
    r RECORD;
    seq INT;
    prev_team UUID := '00000000-0000-0000-0000-000000000000';
BEGIN
    FOR r IN
        SELECT t.id, t.team_id,
               UPPER(LEFT(COALESCE(tm.name, 'TSK'), 3)) AS team_prefix
        FROM team_tasks t
        JOIN agent_teams tm ON tm.id = t.team_id
        WHERE t.identifier IS NULL
        ORDER BY t.team_id, t.created_at
    LOOP
        IF r.team_id != prev_team THEN
            seq := 0;
            prev_team := r.team_id;
        END IF;
        seq := seq + 1;
        UPDATE team_tasks SET task_number = seq, identifier = r.team_prefix || '-' || seq WHERE id = r.id;
    END LOOP;
END $$;

-- ============================================================
-- Part 3: Task followup reminders
-- ============================================================

ALTER TABLE team_tasks ADD COLUMN followup_at       TIMESTAMPTZ;
ALTER TABLE team_tasks ADD COLUMN followup_count    INT NOT NULL DEFAULT 0;
ALTER TABLE team_tasks ADD COLUMN followup_max      INT NOT NULL DEFAULT 0;
ALTER TABLE team_tasks ADD COLUMN followup_message  TEXT;
ALTER TABLE team_tasks ADD COLUMN followup_channel  VARCHAR(60);
ALTER TABLE team_tasks ADD COLUMN followup_chat_id  VARCHAR(255);

CREATE INDEX idx_tt_followup ON team_tasks(followup_at)
  WHERE followup_at IS NOT NULL AND status = 'in_progress';

-- ============================================================
-- Part 4: Fix blocked_by DEFAULT (was NULL, should be empty array)
-- ============================================================

ALTER TABLE team_tasks ALTER COLUMN blocked_by SET DEFAULT '{}'::uuid[];
UPDATE team_tasks SET blocked_by = '{}' WHERE blocked_by IS NULL;

-- ============================================================
-- Part 5: Add team_id to handoff_routes
-- ============================================================

ALTER TABLE handoff_routes ADD COLUMN team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;
CREATE INDEX idx_hr_team ON handoff_routes(team_id) WHERE team_id IS NOT NULL;

-- From 000019_team_id_columns.up.sql
-- Add team_id to memory, KG, traces, and spans tables.
-- Enables future team-scoped memory, KG, and tracing.
-- Nullable: existing rows keep NULL (personal scope). Team-scoped rows will set team_id.

-- Memory documents
ALTER TABLE memory_documents ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_memdoc_team ON memory_documents(team_id) WHERE team_id IS NOT NULL;

-- Memory chunks
ALTER TABLE memory_chunks ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_memchunk_team ON memory_chunks(team_id) WHERE team_id IS NOT NULL;

-- KG entities
ALTER TABLE kg_entities ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_kg_entities_team ON kg_entities(team_id) WHERE team_id IS NOT NULL;

-- KG relations
ALTER TABLE kg_relations ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_kg_relations_team ON kg_relations(team_id) WHERE team_id IS NOT NULL;

-- Traces
ALTER TABLE traces ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_traces_team ON traces(team_id, created_at DESC) WHERE team_id IS NOT NULL;

-- Spans
ALTER TABLE spans ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_spans_team ON spans(team_id) WHERE team_id IS NOT NULL;

-- Cron jobs
ALTER TABLE cron_jobs ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_cron_jobs_team ON cron_jobs(team_id) WHERE team_id IS NOT NULL;

-- Cron run logs
ALTER TABLE cron_run_logs ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_cron_run_logs_team ON cron_run_logs(team_id) WHERE team_id IS NOT NULL;

-- Sessions
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_sessions_team ON sessions(team_id) WHERE team_id IS NOT NULL;

-- Performance indexes for team_tasks
-- GIN index for unblockDependentTasks: WHERE $1 = ANY(blocked_by)
CREATE INDEX IF NOT EXISTS idx_tt_blocked_by ON team_tasks USING GIN(blocked_by);

-- Composite index for ListIdleMembers subquery: NOT EXISTS(... owner_agent_id = ... AND status = ...)
CREATE INDEX IF NOT EXISTS idx_tt_owner_status ON team_tasks(team_id, owner_agent_id, status);

-- From 000020_secure_cli_and_api_keys.up.sql
-- Secure CLI binaries: credential injection for exec tool (Direct Exec Mode).
-- Admin maps binary -> env vars; GoClaw auto-injects into child process.
CREATE TABLE secure_cli_binaries (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    binary_name     TEXT NOT NULL,                          -- display name: "gh", "gcloud"
    binary_path     TEXT,                                   -- resolved absolute path (nullable, auto-resolved at runtime)
    description     TEXT NOT NULL DEFAULT '',
    encrypted_env   BYTEA NOT NULL,                         -- AES-256-GCM encrypted JSON: {"GH_TOKEN":"xxx"}
    deny_args       JSONB NOT NULL DEFAULT '[]',            -- regex patterns: ["auth\\s+", "ssh-key"]
    deny_verbose    JSONB NOT NULL DEFAULT '[]',            -- verbose flag patterns: ["--verbose", "-v"]
    timeout_seconds INTEGER NOT NULL DEFAULT 30,
    tips            TEXT NOT NULL DEFAULT '',                -- hint injected into TOOLS.md context
    agent_id        UUID REFERENCES agents(id) ON DELETE CASCADE,  -- null = global (all agents)
    enabled         BOOLEAN NOT NULL DEFAULT true,
    created_by      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_secure_cli_binary_name ON secure_cli_binaries(binary_name);
CREATE INDEX idx_secure_cli_agent_id ON secure_cli_binaries(agent_id) WHERE agent_id IS NOT NULL;
-- Unique constraint: one binary per agent (with null = global treated as a distinct scope)
CREATE UNIQUE INDEX idx_secure_cli_unique_binary_agent
    ON secure_cli_binaries(binary_name, COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'::uuid));

-- API key management: multiple keys with fine-grained scopes
CREATE TABLE api_keys (
    id            UUID PRIMARY KEY,
    name          VARCHAR(100) NOT NULL,
    prefix        VARCHAR(8)   NOT NULL,              -- first 8 chars for display identification
    key_hash      VARCHAR(64)  NOT NULL UNIQUE,       -- SHA-256 hex digest
    scopes        TEXT[]       NOT NULL DEFAULT '{}',  -- e.g. {'operator.admin','operator.read'}
    expires_at    TIMESTAMPTZ,                         -- NULL = never expires
    last_used_at  TIMESTAMPTZ,
    revoked       BOOLEAN      NOT NULL DEFAULT false,
    created_by    VARCHAR(255),                        -- user ID who created the key
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Fast lookup by hash (only active keys)
CREATE INDEX idx_api_keys_key_hash ON api_keys (key_hash) WHERE NOT revoked;

-- Fast lookup by prefix (for display/search)
CREATE INDEX idx_api_keys_prefix ON api_keys (prefix);

-- From 000021_paired_devices_expiry.up.sql
-- Add expiry to paired devices for defense-in-depth.
-- NULL means no expiry (backward compat for existing rows).
ALTER TABLE paired_devices ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

-- Add confidence_score to team tables for agent self-assessment.
ALTER TABLE team_tasks ADD COLUMN IF NOT EXISTS confidence_score FLOAT;
ALTER TABLE team_messages ADD COLUMN IF NOT EXISTS confidence_score FLOAT;
ALTER TABLE team_task_comments ADD COLUMN IF NOT EXISTS confidence_score FLOAT;

-- From 000022_agent_heartbeats.up.sql
-- Agent heartbeat configuration (per-agent, not per-user).
CREATE TABLE agent_heartbeats (
    id                 UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_id           UUID NOT NULL UNIQUE REFERENCES agents(id) ON DELETE CASCADE,
    enabled            BOOLEAN NOT NULL DEFAULT false,
    interval_sec       INT NOT NULL DEFAULT 1800,
    prompt             TEXT,
    provider_id        UUID REFERENCES llm_providers(id),
    model              VARCHAR(200),
    isolated_session   BOOLEAN NOT NULL DEFAULT true,
    light_context      BOOLEAN NOT NULL DEFAULT false,
    ack_max_chars      INT NOT NULL DEFAULT 300,
    max_retries        INT NOT NULL DEFAULT 2,
    active_hours_start VARCHAR(5),
    active_hours_end   VARCHAR(5),
    timezone           TEXT,
    channel            VARCHAR(50),
    chat_id            TEXT,
    next_run_at        TIMESTAMPTZ,
    last_run_at        TIMESTAMPTZ,
    last_status        VARCHAR(20),
    last_error         TEXT,
    run_count          INT NOT NULL DEFAULT 0,
    suppress_count     INT NOT NULL DEFAULT 0,
    metadata           JSONB DEFAULT '{}',
    created_at         TIMESTAMPTZ DEFAULT NOW(),
    updated_at         TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_heartbeats_due ON agent_heartbeats (next_run_at)
    WHERE enabled = true AND next_run_at IS NOT NULL;

-- Heartbeat execution logs.
CREATE TABLE heartbeat_run_logs (
    id             UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    heartbeat_id   UUID NOT NULL REFERENCES agent_heartbeats(id) ON DELETE CASCADE,
    agent_id       UUID NOT NULL REFERENCES agents(id),
    status         VARCHAR(20) NOT NULL,
    summary        TEXT,
    error          TEXT,
    duration_ms    INT,
    input_tokens   INT DEFAULT 0,
    output_tokens  INT DEFAULT 0,
    skip_reason    VARCHAR(50),
    metadata       JSONB DEFAULT '{}',
    ran_at         TIMESTAMPTZ DEFAULT NOW(),
    created_at     TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_hb_logs_heartbeat ON heartbeat_run_logs (heartbeat_id, ran_at DESC);
CREATE INDEX idx_hb_logs_agent ON heartbeat_run_logs (agent_id, ran_at DESC);

-- Generic agent config permissions (heartbeat, cron, context_files, etc.)
CREATE TABLE agent_config_permissions (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    scope       VARCHAR(100) NOT NULL,
    config_type VARCHAR(50) NOT NULL,
    user_id     VARCHAR(255) NOT NULL,
    permission  VARCHAR(10) NOT NULL,
    granted_by  VARCHAR(255),
    metadata    JSONB DEFAULT '{}',
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    updated_at  TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(agent_id, scope, config_type, user_id)
);

CREATE INDEX idx_acp_lookup ON agent_config_permissions (agent_id, scope, config_type);

-- From 000023_agent_hard_delete_and_file_writer_merge.up.sql
-- ============================================================
-- Part A: File Writer → Config Permissions merge
-- ============================================================

-- Widen scope column to match group_file_writers.group_id size
ALTER TABLE agent_config_permissions ALTER COLUMN scope TYPE VARCHAR(255);

-- Migrate group_file_writers → agent_config_permissions
INSERT INTO agent_config_permissions (agent_id, scope, config_type, user_id, permission, metadata, created_at)
SELECT agent_id, group_id, 'file_writer', user_id, 'allow',
       jsonb_build_object(
         'displayName', COALESCE(display_name, ''),
         'username', COALESCE(username, '')
       ),
       created_at
FROM group_file_writers
ON CONFLICT (agent_id, scope, config_type, user_id) DO NOTHING;

DROP TABLE group_file_writers;

-- ============================================================
-- Part B: Agent hard delete — FK cascade constraints
-- ============================================================

-- Allow soft-deleted agents to reuse agent_key
ALTER TABLE agents DROP CONSTRAINT agents_agent_key_key;
CREATE UNIQUE INDEX idx_agents_agent_key_active ON agents(agent_key) WHERE deleted_at IS NULL;

-- CASCADE: data belongs to agent, delete with it
ALTER TABLE sessions DROP CONSTRAINT sessions_agent_id_fkey;
ALTER TABLE sessions ADD CONSTRAINT sessions_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE;

ALTER TABLE cron_jobs DROP CONSTRAINT cron_jobs_agent_id_fkey;
ALTER TABLE cron_jobs ADD CONSTRAINT cron_jobs_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE;

ALTER TABLE heartbeat_run_logs DROP CONSTRAINT heartbeat_run_logs_agent_id_fkey;
ALTER TABLE heartbeat_run_logs ADD CONSTRAINT heartbeat_run_logs_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE;

ALTER TABLE agent_teams DROP CONSTRAINT agent_teams_lead_agent_id_fkey;
ALTER TABLE agent_teams ADD CONSTRAINT agent_teams_lead_agent_id_fkey
    FOREIGN KEY (lead_agent_id) REFERENCES agents(id) ON DELETE CASCADE;

ALTER TABLE team_messages DROP CONSTRAINT team_messages_from_agent_id_fkey;
ALTER TABLE team_messages ADD CONSTRAINT team_messages_from_agent_id_fkey
    FOREIGN KEY (from_agent_id) REFERENCES agents(id) ON DELETE CASCADE;

ALTER TABLE delegation_history DROP CONSTRAINT delegation_history_source_agent_id_fkey;
ALTER TABLE delegation_history ADD CONSTRAINT delegation_history_source_agent_id_fkey
    FOREIGN KEY (source_agent_id) REFERENCES agents(id) ON DELETE CASCADE;

ALTER TABLE delegation_history DROP CONSTRAINT delegation_history_target_agent_id_fkey;
ALTER TABLE delegation_history ADD CONSTRAINT delegation_history_target_agent_id_fkey
    FOREIGN KEY (target_agent_id) REFERENCES agents(id) ON DELETE CASCADE;

ALTER TABLE team_workspace_files DROP CONSTRAINT team_workspace_files_uploaded_by_fkey;
ALTER TABLE team_workspace_files ADD CONSTRAINT team_workspace_files_uploaded_by_fkey
    FOREIGN KEY (uploaded_by) REFERENCES agents(id) ON DELETE CASCADE;

ALTER TABLE team_workspace_file_versions DROP CONSTRAINT team_workspace_file_versions_uploaded_by_fkey;
ALTER TABLE team_workspace_file_versions ADD CONSTRAINT team_workspace_file_versions_uploaded_by_fkey
    FOREIGN KEY (uploaded_by) REFERENCES agents(id) ON DELETE CASCADE;

ALTER TABLE team_workspace_comments DROP CONSTRAINT team_workspace_comments_agent_id_fkey;
ALTER TABLE team_workspace_comments ADD CONSTRAINT team_workspace_comments_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE;

-- SET NULL: keep row, clear reference
ALTER TABLE cron_run_logs DROP CONSTRAINT cron_run_logs_agent_id_fkey;
ALTER TABLE cron_run_logs ADD CONSTRAINT cron_run_logs_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE SET NULL;

ALTER TABLE team_tasks DROP CONSTRAINT team_tasks_owner_agent_id_fkey;
ALTER TABLE team_tasks ADD CONSTRAINT team_tasks_owner_agent_id_fkey
    FOREIGN KEY (owner_agent_id) REFERENCES agents(id) ON DELETE SET NULL;

ALTER TABLE team_tasks DROP CONSTRAINT team_tasks_created_by_agent_id_fkey;
ALTER TABLE team_tasks ADD CONSTRAINT team_tasks_created_by_agent_id_fkey
    FOREIGN KEY (created_by_agent_id) REFERENCES agents(id) ON DELETE SET NULL;

ALTER TABLE team_messages DROP CONSTRAINT team_messages_to_agent_id_fkey;
ALTER TABLE team_messages ADD CONSTRAINT team_messages_to_agent_id_fkey
    FOREIGN KEY (to_agent_id) REFERENCES agents(id) ON DELETE SET NULL;

ALTER TABLE team_task_comments DROP CONSTRAINT team_task_comments_agent_id_fkey;
ALTER TABLE team_task_comments ADD CONSTRAINT team_task_comments_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE SET NULL;

ALTER TABLE team_task_attachments DROP CONSTRAINT team_task_attachments_added_by_fkey;
ALTER TABLE team_task_attachments ADD CONSTRAINT team_task_attachments_added_by_fkey
    FOREIGN KEY (added_by) REFERENCES agents(id) ON DELETE SET NULL;

-- Clean up previously soft-deleted agents (zombie rows)
-- Placed after CASCADE constraints so related data is auto-cleaned
DELETE FROM agents WHERE deleted_at IS NOT NULL;

-- From 000024_team_attachments_refactor.up.sql
-- Phase 1: Team attachments refactor — drop workspace_files, messages; path-based attachments
-- Also adds denormalized count columns on team_tasks for dashboard performance.

-- 1. Drop old attachments (FK → team_workspace_files)
DROP TABLE IF EXISTS team_task_attachments;

-- 2. Drop workspace sub-tables (FK → team_workspace_files)
DROP TABLE IF EXISTS team_workspace_comments;
DROP TABLE IF EXISTS team_workspace_file_versions;

-- 3. Drop workspace files table
DROP TABLE IF EXISTS team_workspace_files;

-- 4. Drop team messages table (tool removed)
DROP TABLE IF EXISTS team_messages;

-- 5. Create new path-based attachments table
CREATE TABLE team_task_attachments (
    id                   UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    task_id              UUID NOT NULL REFERENCES team_tasks(id) ON DELETE CASCADE,
    team_id              UUID NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
    chat_id              VARCHAR(255) NOT NULL DEFAULT '',
    path                 TEXT NOT NULL,
    file_size            BIGINT NOT NULL DEFAULT 0,
    mime_type            VARCHAR(100) DEFAULT '',
    created_by_agent_id  UUID REFERENCES agents(id),
    created_by_sender_id VARCHAR(255) DEFAULT '',
    metadata             JSONB NOT NULL DEFAULT '{}',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(task_id, path)
);
CREATE INDEX idx_tta_task ON team_task_attachments(task_id);
CREATE INDEX idx_tta_team ON team_task_attachments(team_id);

-- 6. Denormalized count columns for dashboard performance
ALTER TABLE team_tasks ADD COLUMN IF NOT EXISTS comment_count INT NOT NULL DEFAULT 0;
ALTER TABLE team_tasks ADD COLUMN IF NOT EXISTS attachment_count INT NOT NULL DEFAULT 0;

-- 7. Vector embedding for semantic task search (subject only)
ALTER TABLE team_tasks ADD COLUMN IF NOT EXISTS embedding vector(1536);
CREATE INDEX IF NOT EXISTS idx_tt_embedding ON team_tasks USING hnsw (embedding vector_cosine_ops);

-- From 000025_kg_entity_embeddings.up.sql
ALTER TABLE kg_entities ADD COLUMN IF NOT EXISTS embedding vector(1536);
CREATE INDEX IF NOT EXISTS idx_kg_entity_vec ON kg_entities USING hnsw(embedding vector_cosine_ops);

-- From 000026_api_key_user_binding.up.sql
-- API key owner binding for identity enforcement
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS owner_id VARCHAR(255);
COMMENT ON COLUMN api_keys.owner_id IS 'User who owns this key. When set, auth via this key forces user_id = owner_id.';
CREATE INDEX IF NOT EXISTS idx_api_keys_owner_id ON api_keys(owner_id) WHERE owner_id IS NOT NULL;

-- Team user grants for access control
CREATE TABLE IF NOT EXISTS team_user_grants (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id    UUID NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
    user_id    VARCHAR(255) NOT NULL,
    role       VARCHAR(50) NOT NULL DEFAULT 'viewer',
    granted_by VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(team_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_team_user_grants_user ON team_user_grants(user_id);
CREATE INDEX IF NOT EXISTS idx_team_user_grants_team ON team_user_grants(team_id);

-- Drop unused legacy tables
DROP TABLE IF EXISTS handoff_routes;
DROP TABLE IF EXISTS delegation_history;

-- From 000027_tenant_foundation.up.sql
-- Plan 2: Tenant Foundation
-- Master tenant UUID v7: 0193a5b0-7000-7000-8000-000000000001

-- ============================================================
-- Phase A: Create tenants + tenant_users tables
-- ============================================================

CREATE TABLE tenants (
    id         UUID PRIMARY KEY,
    name       VARCHAR(255) NOT NULL,
    slug       VARCHAR(100) NOT NULL UNIQUE,
    status     VARCHAR(20) NOT NULL DEFAULT 'active',
    settings   JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_tenants_slug ON tenants(slug);
CREATE INDEX idx_tenants_status ON tenants(status) WHERE status = 'active';

-- Seed master tenant
VALUES ('0193a5b0-7000-7000-8000-000000000001', 'Master', 'master', 'active');

CREATE TABLE tenant_users (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id      VARCHAR(255) NOT NULL,
    display_name VARCHAR(255),
    role         VARCHAR(20) NOT NULL DEFAULT 'member',
    metadata     JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, user_id)
);

CREATE INDEX idx_tenant_users_user ON tenant_users(user_id);
CREATE INDEX idx_tenant_users_tenant ON tenant_users(tenant_id);

-- ============================================================
-- Phase B: ALTER ADD tenant_id to 30 tables
-- All default to master tenant UUID (PG 11+ metadata-only op)
-- ============================================================

-- Core tables
ALTER TABLE agents ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE sessions ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
-- api_keys: NULLABLE (NULL = system-level cross-tenant key)
ALTER TABLE api_keys ADD COLUMN tenant_id UUID REFERENCES tenants(id);

-- Agent ecosystem
ALTER TABLE agent_shares ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE user_context_files ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE user_agent_profiles ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE user_agent_overrides ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE agent_config_permissions ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE agent_links ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE channel_instances ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Memory + KG
ALTER TABLE memory_documents ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE memory_chunks ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE kg_entities ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE kg_relations ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Skills
ALTER TABLE skills ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE skill_user_grants ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Cron
ALTER TABLE cron_jobs ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Tracing + Activity
ALTER TABLE traces ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE activity_logs ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE usage_snapshots ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- MCP
ALTER TABLE mcp_servers ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE mcp_user_grants ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE mcp_access_requests ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Teams
ALTER TABLE agent_teams ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE team_user_grants ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Pairing + Channels
ALTER TABLE pairing_requests ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE paired_devices ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE channel_pending_messages ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE channel_contacts ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- LLM Providers + Config Secrets
ALTER TABLE llm_providers ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE config_secrets ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Other
ALTER TABLE secure_cli_binaries ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Grant tables
ALTER TABLE agent_context_files ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE skill_agent_grants ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE mcp_agent_grants ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Tasks + Tracing
ALTER TABLE team_tasks ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE spans ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Cache
ALTER TABLE embedding_cache ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Team activity tables
ALTER TABLE agent_team_members ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE team_task_comments ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE team_task_events ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE team_task_attachments ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- ============================================================
-- Phase C: Drop defaults (force explicit tenant_id for new rows)
-- ============================================================

ALTER TABLE agents ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE sessions ALTER COLUMN tenant_id DROP DEFAULT;
-- api_keys: no default to drop (nullable, no DEFAULT set)
ALTER TABLE agent_shares ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE user_context_files ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE user_agent_profiles ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE user_agent_overrides ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE agent_config_permissions ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE agent_links ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE channel_instances ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE memory_documents ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE memory_chunks ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE kg_entities ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE kg_relations ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE skills ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE skill_user_grants ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE cron_jobs ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE traces ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE activity_logs ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE usage_snapshots ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE mcp_servers ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE mcp_user_grants ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE mcp_access_requests ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE agent_teams ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE team_user_grants ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE pairing_requests ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE paired_devices ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE channel_pending_messages ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE channel_contacts ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE llm_providers ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE config_secrets ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE secure_cli_binaries ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE agent_context_files ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE skill_agent_grants ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE mcp_agent_grants ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE team_tasks ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE spans ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE embedding_cache ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE agent_team_members ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE team_task_comments ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE team_task_events ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE team_task_attachments ALTER COLUMN tenant_id DROP DEFAULT;

-- ============================================================
-- Phase D: Indexes
-- ============================================================

-- Per-table tenant indexes
CREATE INDEX idx_agents_tenant ON agents(tenant_id);
CREATE INDEX idx_sessions_tenant ON sessions(tenant_id);
CREATE INDEX idx_api_keys_tenant ON api_keys(tenant_id) WHERE tenant_id IS NOT NULL;
CREATE INDEX idx_agent_shares_tenant ON agent_shares(tenant_id);
CREATE INDEX idx_user_context_files_tenant ON user_context_files(tenant_id);
CREATE INDEX idx_user_agent_profiles_tenant ON user_agent_profiles(tenant_id);
CREATE INDEX idx_user_agent_overrides_tenant ON user_agent_overrides(tenant_id);
CREATE INDEX idx_agent_config_permissions_tenant ON agent_config_permissions(tenant_id);
CREATE INDEX idx_agent_links_tenant ON agent_links(tenant_id);
CREATE INDEX idx_channel_instances_tenant ON channel_instances(tenant_id);
CREATE INDEX idx_memory_documents_tenant ON memory_documents(tenant_id);
CREATE INDEX idx_memory_chunks_tenant ON memory_chunks(tenant_id);
CREATE INDEX idx_kg_entities_tenant ON kg_entities(tenant_id);
CREATE INDEX idx_kg_relations_tenant ON kg_relations(tenant_id);
CREATE INDEX idx_skills_tenant ON skills(tenant_id);
CREATE INDEX idx_skill_user_grants_tenant ON skill_user_grants(tenant_id);
CREATE INDEX idx_cron_jobs_tenant ON cron_jobs(tenant_id);
CREATE INDEX idx_traces_tenant ON traces(tenant_id);
CREATE INDEX idx_activity_logs_tenant ON activity_logs(tenant_id);
CREATE INDEX idx_usage_snapshots_tenant ON usage_snapshots(tenant_id);
CREATE INDEX idx_mcp_servers_tenant ON mcp_servers(tenant_id);
CREATE INDEX idx_mcp_user_grants_tenant ON mcp_user_grants(tenant_id);
CREATE INDEX idx_mcp_access_requests_tenant ON mcp_access_requests(tenant_id);
CREATE INDEX idx_agent_teams_tenant ON agent_teams(tenant_id);
CREATE INDEX idx_team_user_grants_tenant ON team_user_grants(tenant_id);
CREATE INDEX idx_pairing_requests_tenant ON pairing_requests(tenant_id);
CREATE INDEX idx_paired_devices_tenant ON paired_devices(tenant_id);
CREATE INDEX idx_channel_pending_messages_tenant ON channel_pending_messages(tenant_id);
CREATE INDEX idx_channel_contacts_tenant ON channel_contacts(tenant_id);
CREATE INDEX idx_llm_providers_tenant ON llm_providers(tenant_id);
CREATE INDEX idx_config_secrets_tenant ON config_secrets(tenant_id);
CREATE INDEX idx_secure_cli_binaries_tenant ON secure_cli_binaries(tenant_id);
CREATE INDEX idx_agent_context_files_tenant ON agent_context_files(tenant_id);
CREATE INDEX idx_skill_agent_grants_tenant ON skill_agent_grants(tenant_id);
CREATE INDEX idx_mcp_agent_grants_tenant ON mcp_agent_grants(tenant_id);
CREATE INDEX idx_team_tasks_tenant ON team_tasks(tenant_id);
CREATE INDEX idx_spans_tenant ON spans(tenant_id);
CREATE INDEX idx_embedding_cache_tenant ON embedding_cache(tenant_id);
CREATE INDEX idx_agent_team_members_tenant ON agent_team_members(tenant_id);
CREATE INDEX idx_team_task_comments_tenant ON team_task_comments(tenant_id);
CREATE INDEX idx_team_task_events_tenant ON team_task_events(tenant_id);
CREATE INDEX idx_team_task_attachments_tenant ON team_task_attachments(tenant_id);

-- Composite indexes for Plan 3 query performance
CREATE INDEX idx_agents_tenant_active ON agents(tenant_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_sessions_tenant_user ON sessions(tenant_id, user_id);
CREATE INDEX idx_traces_tenant_time ON traces(tenant_id, created_at DESC);

-- ============================================================
-- Phase E: Seed master tenant owner from existing agents
-- ============================================================

SELECT DISTINCT '0193a5b0-7000-7000-8000-000000000001'::uuid, owner_id, 'owner'
FROM agents
WHERE owner_id IS NOT NULL AND owner_id != ''
LIMIT 1
ON CONFLICT (tenant_id, user_id) DO NOTHING;

-- ============================================================
-- Phase F: DROP custom_tools table (dead code — agent loop never wired)
-- ============================================================

DROP TABLE IF EXISTS custom_tools;

-- ============================================================
-- Phase G: Per-tenant builtin tool config overrides
-- ============================================================

CREATE TABLE builtin_tool_tenant_configs (
    tool_name  VARCHAR(100) NOT NULL REFERENCES builtin_tools(name) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    enabled    BOOLEAN,
    settings   JSONB,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tool_name, tenant_id)
);

CREATE INDEX idx_builtin_tool_tenant_configs_tenant ON builtin_tool_tenant_configs(tenant_id);

-- ============================================================
-- Phase H: Per-tenant skill config (disable system skills)
-- ============================================================

CREATE TABLE skill_tenant_configs (
    skill_id   UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    enabled    BOOLEAN NOT NULL DEFAULT true,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (skill_id, tenant_id)
);

CREATE INDEX idx_skill_tenant_configs_tenant ON skill_tenant_configs(tenant_id);

-- ============================================================
-- Phase J: MCP per-user credentials
-- ============================================================

CREATE TABLE mcp_user_credentials (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id  UUID NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    user_id    VARCHAR(255) NOT NULL,
    api_key    TEXT,
    headers    BYTEA,
    env        BYTEA,
    tenant_id  UUID NOT NULL REFERENCES tenants(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(server_id, user_id, tenant_id)
);

CREATE INDEX idx_mcp_user_credentials_tenant ON mcp_user_credentials(tenant_id);
CREATE INDEX idx_mcp_user_credentials_server ON mcp_user_credentials(server_id);

-- ============================================================
-- Phase I: Update UNIQUE constraints to include tenant_id
-- Allows same name/key/slug across different tenants.
-- ============================================================

-- agents.agent_key: (agent_key) → (tenant_id, agent_key)
-- Old constraint replaced by partial index in migration 23.
DROP INDEX IF EXISTS idx_agents_agent_key_active;
CREATE UNIQUE INDEX idx_agents_tenant_agent_key_active ON agents(tenant_id, agent_key) WHERE deleted_at IS NULL;

-- sessions.session_key: globally unique → (tenant_id, session_key)
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_session_key_key;
DROP INDEX IF EXISTS sessions_session_key_key;
CREATE UNIQUE INDEX idx_sessions_tenant_session_key ON sessions(tenant_id, session_key);

-- skills.slug: globally unique → (tenant_id, slug)
ALTER TABLE skills DROP CONSTRAINT IF EXISTS skills_slug_key;
DROP INDEX IF EXISTS skills_slug_key;
CREATE UNIQUE INDEX idx_skills_tenant_slug ON skills(tenant_id, slug);

-- mcp_servers.name: globally unique → (tenant_id, name)
ALTER TABLE mcp_servers DROP CONSTRAINT IF EXISTS mcp_servers_name_key;
DROP INDEX IF EXISTS mcp_servers_name_key;
CREATE UNIQUE INDEX idx_mcp_servers_tenant_name ON mcp_servers(tenant_id, name);

-- channel_contacts: (channel_type, sender_id) → (tenant_id, channel_type, sender_id)
ALTER TABLE channel_contacts DROP CONSTRAINT IF EXISTS channel_contacts_channel_type_sender_id_key;
DROP INDEX IF EXISTS channel_contacts_channel_type_sender_id_key;
CREATE UNIQUE INDEX idx_channel_contacts_tenant_type_sender ON channel_contacts(tenant_id, channel_type, sender_id);

-- llm_providers.name: globally unique → (tenant_id, name)
ALTER TABLE llm_providers DROP CONSTRAINT IF EXISTS llm_providers_name_key;
DROP INDEX IF EXISTS llm_providers_name_key;
CREATE UNIQUE INDEX idx_llm_providers_tenant_name ON llm_providers(tenant_id, name);

-- config_secrets.key: PK (key) → PK (key, tenant_id)
ALTER TABLE config_secrets DROP CONSTRAINT IF EXISTS config_secrets_pkey;
ALTER TABLE config_secrets ADD PRIMARY KEY (key, tenant_id);

-- paired_devices: UNIQUE (sender_id, channel) → (tenant_id, sender_id, channel)
ALTER TABLE paired_devices DROP CONSTRAINT IF EXISTS paired_devices_sender_id_channel_key;
DROP INDEX IF EXISTS paired_devices_sender_id_channel_key;
CREATE UNIQUE INDEX idx_paired_devices_tenant_sender_channel ON paired_devices(tenant_id, sender_id, channel);

-- channel_instances.name: globally unique → (tenant_id, name)
ALTER TABLE channel_instances DROP CONSTRAINT IF EXISTS channel_instances_name_key;
DROP INDEX IF EXISTS channel_instances_name_key;
CREATE UNIQUE INDEX idx_channel_instances_tenant_name ON channel_instances(tenant_id, name);

-- usage_snapshots: add tenant_id to unique conflict index for per-tenant aggregation
DROP INDEX IF EXISTS idx_usage_snapshots_unique;
CREATE UNIQUE INDEX idx_usage_snapshots_unique ON usage_snapshots (
    bucket_hour,
    COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'::uuid),
    provider, model, channel,
    tenant_id
);

-- Cleanup: strip leaked gateway tokens from session media URLs.
-- Old code embedded ?token=GATEWAY_TOKEN in markdown image URLs stored in session messages.
-- New code stores clean paths; frontend adds auth at render time.
UPDATE sessions
SET messages = regexp_replace(messages::text, '\?token=[a-f0-9]+', '', 'g')::jsonb
WHERE messages::text LIKE '%?token=%';

-- ============================================================
-- Phase K: Migrate remaining UUID v4 defaults to v7
-- ============================================================

ALTER TABLE kg_entities          ALTER COLUMN id SET DEFAULT uuid_generate_v7();
ALTER TABLE kg_relations         ALTER COLUMN id SET DEFAULT uuid_generate_v7();
ALTER TABLE channel_contacts     ALTER COLUMN id SET DEFAULT uuid_generate_v7();
ALTER TABLE team_user_grants     ALTER COLUMN id SET DEFAULT uuid_generate_v7();
ALTER TABLE mcp_user_credentials ALTER COLUMN id SET DEFAULT uuid_generate_v7();

-- From 000028_comment_type.up.sql
-- Add comment_type to distinguish note vs blocker comments.
-- Blocker comments trigger task auto-fail + leader escalation.
ALTER TABLE team_task_comments ADD COLUMN IF NOT EXISTS comment_type VARCHAR(20) NOT NULL DEFAULT 'note';

-- From 000029_system_configs.up.sql
-- system_configs: centralized key-value store for per-tenant system settings.
-- Each tenant has its own config entries. Falls back to master tenant at app layer.
-- Plain TEXT value (not encrypted). Use config_secrets for secrets.
CREATE TABLE IF NOT EXISTS system_configs (
    key        VARCHAR(100) NOT NULL,
    value      TEXT NOT NULL,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (key, tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_system_configs_tenant ON system_configs(tenant_id);

-- From 000030_jsonb_gin_indexes.up.sql
-- GIN indexes for JSONB columns that are actively queried with -> / ->> operators.
-- Audit found 0 existing GIN indexes on 30+ JSONB columns.

-- spans.metadata: used for cache token aggregation (tracing.go, snapshot_worker.go)
-- and chatgpt_oauth_routing evidence queries (agents_codex_pool_activity.go).
-- Partial index on span_type = 'llm_call' to reduce INSERT overhead on high-volume table.
CREATE INDEX IF NOT EXISTS idx_spans_metadata_gin
  ON spans USING GIN (metadata)
  WHERE span_type = 'llm_call';

-- sessions.metadata: used for chat_title filtering in pending message lookups
-- (pending_message_store.go) and heartbeat queries (heartbeat.go).
CREATE INDEX IF NOT EXISTS idx_sessions_metadata_gin
  ON sessions USING GIN (metadata);

-- From 000031_kg_fts_and_dedup.up.sql
-- tsvector full-text search for KG entities (replaces ILIKE)
ALTER TABLE kg_entities ADD COLUMN IF NOT EXISTS tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', name || ' ' || COALESCE(description, ''))) STORED;

CREATE INDEX IF NOT EXISTS idx_kg_entities_tsv ON kg_entities USING GIN (tsv);

-- Dedup candidates table for entity deduplication review
CREATE TABLE IF NOT EXISTS kg_dedup_candidates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id VARCHAR(255) NOT NULL DEFAULT '',
    entity_a_id UUID NOT NULL REFERENCES kg_entities(id) ON DELETE CASCADE,
    entity_b_id UUID NOT NULL REFERENCES kg_entities(id) ON DELETE CASCADE,
    similarity FLOAT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(entity_a_id, entity_b_id)
);

CREATE INDEX IF NOT EXISTS idx_kg_dedup_agent ON kg_dedup_candidates(agent_id, status);

-- From 000032_secure_cli_user_credentials.up.sql
-- Per-user credentials for secure CLI binaries.
-- Mirrors mcp_user_credentials pattern: user-specific env vars override binary defaults.
CREATE TABLE secure_cli_user_credentials (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    binary_id     UUID NOT NULL REFERENCES secure_cli_binaries(id) ON DELETE CASCADE,
    user_id       VARCHAR(255) NOT NULL,
    encrypted_env BYTEA NOT NULL,  -- AES-256-GCM encrypted JSON: {"GH_TOKEN":"xxx"}
    metadata      JSONB NOT NULL DEFAULT '{}',
    tenant_id     UUID NOT NULL REFERENCES tenants(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(binary_id, user_id, tenant_id)
);

CREATE INDEX idx_scuc_tenant ON secure_cli_user_credentials(tenant_id);
CREATE INDEX idx_scuc_binary ON secure_cli_user_credentials(binary_id);

-- Add contact_type column to channel_contacts to distinguish user vs group contacts.
-- Default "user" for backward compatibility with existing records.
ALTER TABLE channel_contacts ADD COLUMN IF NOT EXISTS contact_type VARCHAR(20) NOT NULL DEFAULT 'user';

-- From 000033_cron_payload_columns.up.sql
ALTER TABLE cron_jobs ADD COLUMN stateless BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE cron_jobs ADD COLUMN deliver BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE cron_jobs ADD COLUMN deliver_channel TEXT NOT NULL DEFAULT '';
ALTER TABLE cron_jobs ADD COLUMN deliver_to TEXT NOT NULL DEFAULT '';
ALTER TABLE cron_jobs ADD COLUMN wake_heartbeat BOOLEAN NOT NULL DEFAULT false;

UPDATE cron_jobs SET
  deliver = COALESCE((payload->>'deliver')::boolean, false),
  deliver_channel = COALESCE(payload->>'channel', ''),
  deliver_to = COALESCE(payload->>'to', ''),
  wake_heartbeat = COALESCE((payload->>'wake_heartbeat')::boolean, false)
WHERE payload IS NOT NULL;

UPDATE cron_jobs SET payload = payload - 'deliver' - 'channel' - 'to' - 'wake_heartbeat'
WHERE payload IS NOT NULL;

-- From 000034_subagent_tasks.up.sql
-- Persist subagent task lifecycle for audit trail, cost attribution, and restart recovery.
CREATE TABLE IF NOT EXISTS subagent_tasks (
    id                UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    parent_agent_key  VARCHAR(255) NOT NULL,
    session_key       VARCHAR(500),
    subject           VARCHAR(255) NOT NULL,
    description       TEXT NOT NULL,
    status            VARCHAR(20) NOT NULL DEFAULT 'running',
    result            TEXT,
    depth             INT NOT NULL DEFAULT 1,
    model             VARCHAR(255),
    provider          VARCHAR(255),
    iterations        INT NOT NULL DEFAULT 0,
    input_tokens      BIGINT NOT NULL DEFAULT 0,
    output_tokens     BIGINT NOT NULL DEFAULT 0,
    origin_channel    VARCHAR(50),
    origin_chat_id    VARCHAR(255),
    origin_peer_kind  VARCHAR(20),
    origin_user_id    VARCHAR(255),
    spawned_by        UUID,
    completed_at      TIMESTAMPTZ,
    archived_at       TIMESTAMPTZ,
    metadata          JSONB NOT NULL DEFAULT '{}',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Primary lookup: roster by parent + status.
CREATE INDEX idx_subagent_tasks_parent_status
    ON subagent_tasks(tenant_id, parent_agent_key, status);

-- Session-scoped lookup.
CREATE INDEX idx_subagent_tasks_session
    ON subagent_tasks(session_key) WHERE session_key IS NOT NULL;

-- Time-based audit & cleanup.
CREATE INDEX idx_subagent_tasks_created
    ON subagent_tasks(tenant_id, created_at DESC);

-- Flexible metadata queries.
CREATE INDEX idx_subagent_tasks_metadata_gin
    ON subagent_tasks USING GIN (metadata);

-- Archival candidates.
CREATE INDEX idx_subagent_tasks_archive
    ON subagent_tasks(status, completed_at)
    WHERE status IN ('completed', 'failed', 'cancelled') AND archived_at IS NULL;

-- From 000035_contact_thread_id.up.sql
ALTER TABLE channel_contacts ADD COLUMN thread_id VARCHAR(100);
ALTER TABLE channel_contacts ADD COLUMN thread_type VARCHAR(20);

-- Fix sender_id: strip "|username" suffix, keep only numeric ID.
-- Step 1: Delete "|username" rows where a numeric-only row already exists (avoid UNIQUE conflict).
DELETE FROM channel_contacts
WHERE sender_id LIKE '%|%'
  AND EXISTS (
    SELECT 1 FROM channel_contacts c2
    WHERE c2.tenant_id = channel_contacts.tenant_id
      AND c2.channel_type = channel_contacts.channel_type
      AND c2.sender_id = split_part(channel_contacts.sender_id, '|', 1)
      AND COALESCE(c2.thread_id, '') = COALESCE(channel_contacts.thread_id, '')
  );
-- Step 2: Update remaining "|username" rows (no numeric counterpart) to strip suffix.
UPDATE channel_contacts
SET sender_id = split_part(sender_id, '|', 1)
WHERE sender_id LIKE '%|%';

DROP INDEX IF EXISTS idx_channel_contacts_tenant_type_sender;
CREATE UNIQUE INDEX idx_channel_contacts_tenant_type_sender
  ON channel_contacts (tenant_id, channel_type, sender_id, COALESCE(thread_id, ''));

-- From 000036_secure_cli_agent_grants.up.sql
-- Per-agent grants for secure CLI binaries with optional setting overrides.
-- Separates "which agents can use a binary" from "binary credential definition".

-- 1. Create agent grants table
CREATE TABLE secure_cli_agent_grants (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    binary_id       UUID NOT NULL REFERENCES secure_cli_binaries(id) ON DELETE CASCADE,
    agent_id        UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    deny_args       JSONB,              -- NULL = use binary default
    deny_verbose    JSONB,              -- NULL = use binary default
    timeout_seconds INTEGER,            -- NULL = use binary default
    tips            TEXT,               -- NULL = use binary default
    enabled         BOOLEAN NOT NULL DEFAULT true,
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(binary_id, agent_id, tenant_id)
);

CREATE INDEX idx_scag_binary ON secure_cli_agent_grants(binary_id);
CREATE INDEX idx_scag_agent ON secure_cli_agent_grants(agent_id);
CREATE INDEX idx_scag_tenant ON secure_cli_agent_grants(tenant_id);

-- 2. Add is_global column (default true for backward compat)
ALTER TABLE secure_cli_binaries ADD COLUMN is_global BOOLEAN NOT NULL DEFAULT true;

-- 3. Migrate agent-specific rows to grants table.
--    Copy settings from agent-specific binaries into grants.
INSERT INTO secure_cli_agent_grants (binary_id, agent_id, deny_args, deny_verbose, timeout_seconds, tips, enabled, tenant_id)
SELECT id, agent_id, deny_args, deny_verbose, timeout_seconds, tips, enabled, tenant_id
FROM secure_cli_binaries
WHERE agent_id IS NOT NULL;

-- 4. For agent-specific rows that HAVE a matching global row (same binary_name + tenant):
--    Re-point grants to the global row, then delete the now-orphaned agent-specific row.
UPDATE secure_cli_agent_grants g
SET binary_id = sub.global_id
FROM (
    SELECT g2.id AS grant_id, b_global.id AS global_id
    FROM secure_cli_agent_grants g2
    JOIN secure_cli_binaries b_agent ON b_agent.id = g2.binary_id AND b_agent.agent_id IS NOT NULL
    JOIN secure_cli_binaries b_global ON b_global.binary_name = b_agent.binary_name
        AND b_global.tenant_id = b_agent.tenant_id
        AND b_global.agent_id IS NULL
) sub
WHERE g.id = sub.grant_id;

DELETE FROM secure_cli_binaries
WHERE agent_id IS NOT NULL
  AND EXISTS (
    SELECT 1 FROM secure_cli_binaries b2
    WHERE b2.binary_name = secure_cli_binaries.binary_name
      AND b2.tenant_id = secure_cli_binaries.tenant_id
      AND b2.agent_id IS NULL
  );

-- 5. For agent-specific rows WITHOUT a global counterpart:
--    Dedup: keep the row with the smallest id per (binary_name, tenant_id) as the
--    canonical binary definition. Re-point grants from duplicates to the keeper, then delete dupes.

-- 5a. Re-point grants from duplicate rows to the canonical (MIN id) row.
UPDATE secure_cli_agent_grants g
SET binary_id = keeper.keeper_id
FROM (
    SELECT b.id AS dup_id, first_value(b.id) OVER (
        PARTITION BY b.binary_name, b.tenant_id ORDER BY b.id
    ) AS keeper_id
    FROM secure_cli_binaries b
    WHERE b.agent_id IS NOT NULL
) keeper
WHERE g.binary_id = keeper.dup_id
  AND keeper.dup_id != keeper.keeper_id;

-- 5b. Delete duplicate rows (keep only the canonical per binary_name+tenant_id).
DELETE FROM secure_cli_binaries
WHERE agent_id IS NOT NULL
  AND id NOT IN (
    SELECT DISTINCT ON (binary_name, tenant_id) id
    FROM secure_cli_binaries
    WHERE agent_id IS NOT NULL
    ORDER BY binary_name, tenant_id, id
  );

-- 5c. Mark remaining agent-specific rows as restricted (is_global = false).
UPDATE secure_cli_binaries SET is_global = false
WHERE agent_id IS NOT NULL;

-- 6. Drop agent_id column and old indexes.
DROP INDEX IF EXISTS idx_secure_cli_unique_binary_agent;
DROP INDEX IF EXISTS idx_secure_cli_agent_id;
ALTER TABLE secure_cli_binaries DROP COLUMN agent_id;

-- 7. New unique constraint: one binary per name per tenant.
CREATE UNIQUE INDEX idx_secure_cli_unique_binary_tenant
    ON secure_cli_binaries(binary_name, tenant_id);

-- From 000037_v3_memory_evolution.up.sql
-- V3 Core: Memory, Evolution, KG temporal
-- Migration 000037

-- Episodic summaries (Tier 2 memory)
CREATE TABLE episodic_summaries (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id     VARCHAR(255) NOT NULL DEFAULT '',
    session_key TEXT NOT NULL,

    summary      TEXT NOT NULL,
    l0_abstract  TEXT NOT NULL DEFAULT '',
    key_topics   TEXT[] DEFAULT '{}',
    embedding    vector(1536),
    source_type  TEXT NOT NULL DEFAULT 'session',
    source_id    TEXT,
    turn_count  INT NOT NULL DEFAULT 0,
    token_count INT NOT NULL DEFAULT 0,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ
);

CREATE INDEX idx_episodic_agent_user ON episodic_summaries(agent_id, user_id);
CREATE INDEX idx_episodic_tenant ON episodic_summaries(tenant_id);
CREATE UNIQUE INDEX idx_episodic_source_dedup ON episodic_summaries(agent_id, user_id, source_id)
    WHERE source_id IS NOT NULL;
CREATE INDEX idx_episodic_tsv ON episodic_summaries USING GIN(to_tsvector('simple', summary));
CREATE INDEX idx_episodic_vec ON episodic_summaries USING hnsw(embedding vector_cosine_ops)
    WHERE embedding IS NOT NULL;
CREATE INDEX idx_episodic_expires ON episodic_summaries(expires_at) WHERE expires_at IS NOT NULL;

-- Evolution metrics (Stage 1 self-evolution)
CREATE TABLE agent_evolution_metrics (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    session_key TEXT NOT NULL,

    metric_type TEXT NOT NULL,
    metric_key  TEXT NOT NULL,
    value       JSONB NOT NULL,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_evo_metrics_agent_type ON agent_evolution_metrics(agent_id, metric_type);
CREATE INDEX idx_evo_metrics_created ON agent_evolution_metrics(created_at);
CREATE INDEX idx_evo_metrics_tenant ON agent_evolution_metrics(tenant_id);

-- Evolution suggestions (Stage 2 self-evolution)
CREATE TABLE agent_evolution_suggestions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    agent_id        UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,

    suggestion_type TEXT NOT NULL,
    suggestion      TEXT NOT NULL,
    rationale       TEXT NOT NULL,
    parameters      JSONB,

    status          TEXT NOT NULL DEFAULT 'pending',
    reviewed_by     TEXT,
    reviewed_at     TIMESTAMPTZ,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_evo_suggestions_agent ON agent_evolution_suggestions(agent_id, status);
CREATE INDEX idx_evo_suggestions_tenant ON agent_evolution_suggestions(tenant_id);

-- KG temporal validity windows
ALTER TABLE kg_entities ADD COLUMN IF NOT EXISTS valid_from TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE kg_entities ADD COLUMN IF NOT EXISTS valid_until TIMESTAMPTZ;

ALTER TABLE kg_relations ADD COLUMN IF NOT EXISTS valid_from TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE kg_relations ADD COLUMN IF NOT EXISTS valid_until TIMESTAMPTZ;

CREATE INDEX idx_kg_entities_current ON kg_entities(agent_id, user_id)
    WHERE valid_until IS NULL;
CREATE INDEX idx_kg_entities_temporal ON kg_entities(agent_id, user_id, valid_from, valid_until);

CREATE INDEX idx_kg_relations_current ON kg_relations(agent_id, user_id)
    WHERE valid_until IS NULL;
CREATE INDEX idx_kg_relations_temporal ON kg_relations(agent_id, user_id, valid_from, valid_until);

-- Promote well-known fields from agents.other_config JSONB to dedicated columns

-- 7 scalar columns
ALTER TABLE agents
  ADD COLUMN IF NOT EXISTS emoji TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS agent_description TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS thinking_level TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS max_tokens INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS self_evolve BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS skill_evolve BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS skill_nudge_interval INT NOT NULL DEFAULT 0;

-- 5 nested JSONB columns (structs that stay JSON-shaped)
ALTER TABLE agents
  ADD COLUMN IF NOT EXISTS reasoning_config JSONB NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS workspace_sharing JSONB NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS chatgpt_oauth_routing JSONB NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS shell_deny_groups JSONB NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS kg_dedup_config JSONB NOT NULL DEFAULT '{}';

-- Backfill from other_config
UPDATE agents SET
  emoji = COALESCE(other_config->>'emoji', ''),
  agent_description = COALESCE(other_config->>'description', ''),
  thinking_level = COALESCE(other_config->>'thinking_level', ''),
  max_tokens = COALESCE((other_config->>'max_tokens')::int, 0),
  self_evolve = COALESCE((other_config->>'self_evolve')::boolean, false),
  skill_evolve = COALESCE((other_config->>'skill_evolve')::boolean, false),
  skill_nudge_interval = COALESCE((other_config->>'skill_nudge_interval')::int, 0),
  reasoning_config = COALESCE(other_config->'reasoning', '{}'),
  workspace_sharing = COALESCE(other_config->'workspace_sharing', '{}'),
  chatgpt_oauth_routing = COALESCE(other_config->'chatgpt_oauth_routing', '{}'),
  shell_deny_groups = COALESCE(other_config->'shell_deny_groups', '{}'),
  kg_dedup_config = COALESCE(other_config->'kg_dedup_config', '{}')
WHERE other_config != '{}' AND other_config IS NOT NULL;

-- Clean promoted keys from other_config
UPDATE agents SET other_config = other_config
  - 'emoji' - 'description' - 'thinking_level' - 'max_tokens'
  - 'self_evolve' - 'skill_evolve' - 'skill_nudge_interval'
  - 'reasoning' - 'workspace_sharing' - 'chatgpt_oauth_routing'
  - 'shell_deny_groups' - 'kg_dedup_config';

-- From 000038_vault_tables.up.sql
-- vault_documents: document registry for Knowledge Vault.
-- Metadata pointers: FS holds content, DB holds path + hash + embedding + links.
CREATE TABLE vault_documents (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id     UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    scope        TEXT NOT NULL DEFAULT 'personal',
    path         TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    doc_type     TEXT NOT NULL DEFAULT 'note',
    content_hash TEXT NOT NULL DEFAULT '',
    embedding    vector(1536),
    metadata     JSONB DEFAULT '{}',
    created_at   TIMESTAMPTZ DEFAULT NOW(),
    updated_at   TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(agent_id, scope, path)
);

CREATE INDEX idx_vault_docs_tenant ON vault_documents(tenant_id);
CREATE INDEX idx_vault_docs_agent_scope ON vault_documents(agent_id, scope);
CREATE INDEX idx_vault_docs_type ON vault_documents(agent_id, doc_type);
CREATE INDEX idx_vault_docs_hash ON vault_documents(content_hash);
CREATE INDEX idx_vault_docs_embedding ON vault_documents
    USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64);

-- FTS on title + path for keyword search.
ALTER TABLE vault_documents ADD COLUMN tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', coalesce(title,'') || ' ' || coalesce(path,''))) STORED;
CREATE INDEX idx_vault_docs_tsv ON vault_documents USING gin(tsv);

-- vault_links: bidirectional links between docs (wikilinks).
CREATE TABLE vault_links (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_doc_id UUID NOT NULL REFERENCES vault_documents(id) ON DELETE CASCADE,
    to_doc_id   UUID NOT NULL REFERENCES vault_documents(id) ON DELETE CASCADE,
    link_type   TEXT NOT NULL DEFAULT 'wikilink',
    context     TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(from_doc_id, to_doc_id, link_type)
);

CREATE INDEX idx_vault_links_from ON vault_links(from_doc_id);
CREATE INDEX idx_vault_links_to ON vault_links(to_doc_id);

-- vault_versions: v3.1 prep (empty for now, schema only).
CREATE TABLE vault_versions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    doc_id     UUID NOT NULL REFERENCES vault_documents(id) ON DELETE CASCADE,
    version    INT NOT NULL DEFAULT 1,
    content    TEXT NOT NULL DEFAULT '',
    changed_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(doc_id, version)
);

-- From 000039_episodic_summaries.up.sql
-- episodic_summaries table + indexes already created in migration 000037.
-- This migration only clears stale agent_links data.

-- Clear all agent_links. Teams use agent_team_members directly;
-- delegate tool (v3) will use explicit links created via API.
TRUNCATE agent_links;

-- From 000040_episodic_search_index.up.sql
-- Migration 000040: Episodic search index
-- Adds stored tsvector column for full-text search and an optimized HNSW vector index.
-- Note: promoted_at column is added in migration 000041.

-- Immutable wrapper: array_to_string is STABLE in PG, but the expression is
-- effectively immutable for our use (no locale-dependent behavior on text[]).
-- Generated columns require IMMUTABLE expressions.
CREATE OR REPLACE FUNCTION immutable_array_to_string(arr text[], sep text)
RETURNS text LANGUAGE sql IMMUTABLE PARALLEL SAFE AS
$$SELECT array_to_string(arr, sep)$$;

ALTER TABLE episodic_summaries ADD COLUMN IF NOT EXISTS search_vector tsvector
  GENERATED ALWAYS AS (to_tsvector('english'::regconfig, coalesce(summary, '') || ' ' || coalesce(immutable_array_to_string(key_topics, ' '), ''))) STORED;

CREATE INDEX IF NOT EXISTS idx_episodic_search_vector ON episodic_summaries USING GIN (search_vector);
CREATE INDEX IF NOT EXISTS idx_episodic_embedding_hnsw ON episodic_summaries USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64) WHERE embedding IS NOT NULL;

-- From 000041_episodic_promoted.up.sql
-- Add promoted_at column to episodic_summaries for dreaming pipeline.
-- NULL = not yet promoted to long-term memory; NOT NULL = already processed.
ALTER TABLE episodic_summaries ADD COLUMN IF NOT EXISTS promoted_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_episodic_unpromoted
    ON episodic_summaries(agent_id, user_id, created_at)
    WHERE promoted_at IS NULL;

-- From 000042_vault_tsv_summary.up.sql
-- Add summary column + include in FTS index for richer search.
ALTER TABLE vault_documents ADD COLUMN IF NOT EXISTS summary TEXT NOT NULL DEFAULT '';

-- Re-create tsvector to include summary.
ALTER TABLE vault_documents DROP COLUMN IF EXISTS tsv;
ALTER TABLE vault_documents ADD COLUMN tsv tsvector
    GENERATED ALWAYS AS (
        to_tsvector('simple',
            coalesce(title, '') || ' ' ||
            coalesce(path, '') || ' ' ||
            coalesce(summary, '')
        )
    ) STORED;
CREATE INDEX IF NOT EXISTS idx_vault_docs_tsv ON vault_documents USING gin(tsv);

-- From 000043_vault_team_custom_scope.up.sql
-- Add team_id to vault_documents (NULL = personal scope).
ALTER TABLE vault_documents ADD COLUMN IF NOT EXISTS team_id UUID
    REFERENCES agent_teams(id) ON DELETE SET NULL;

-- Add custom_scope for future flexibility.
ALTER TABLE vault_documents ADD COLUMN IF NOT EXISTS custom_scope VARCHAR(255);

-- Drop old broken UNIQUE constraint that causes cross-team data corruption.
ALTER TABLE vault_documents DROP CONSTRAINT IF EXISTS vault_documents_agent_id_scope_path_key;

-- New UNIQUE with COALESCE: NULL team_id maps to nil-UUID so NULLs collapse correctly.
CREATE UNIQUE INDEX IF NOT EXISTS uq_vault_docs_agent_team_scope_path
    ON vault_documents (agent_id, COALESCE(team_id, '00000000-0000-0000-0000-000000000000'), scope, path);

-- Index for team_id filtering.
CREATE INDEX IF NOT EXISTS idx_vault_docs_team ON vault_documents(team_id) WHERE team_id IS NOT NULL;

-- Trigger: when team deleted (ON DELETE SET NULL), auto-correct scope to 'personal'.
-- Prevents orphaned scope='team' docs.
CREATE OR REPLACE FUNCTION vault_docs_team_null_scope_fix()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.team_id IS NULL AND OLD.team_id IS NOT NULL THEN
        NEW.scope := 'personal';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_vault_docs_team_null_scope
    BEFORE UPDATE OF team_id ON vault_documents
    FOR EACH ROW
    EXECUTE FUNCTION vault_docs_team_null_scope_fix();

-- Add custom_scope to 9 other tables.
ALTER TABLE vault_links ADD COLUMN IF NOT EXISTS custom_scope VARCHAR(255);
ALTER TABLE vault_versions ADD COLUMN IF NOT EXISTS custom_scope VARCHAR(255);
ALTER TABLE memory_documents ADD COLUMN IF NOT EXISTS custom_scope VARCHAR(255);
ALTER TABLE memory_chunks ADD COLUMN IF NOT EXISTS custom_scope VARCHAR(255);
ALTER TABLE team_tasks ADD COLUMN IF NOT EXISTS custom_scope VARCHAR(255);
ALTER TABLE team_task_attachments ADD COLUMN IF NOT EXISTS custom_scope VARCHAR(255);
ALTER TABLE team_task_comments ADD COLUMN IF NOT EXISTS custom_scope VARCHAR(255);
ALTER TABLE team_task_events ADD COLUMN IF NOT EXISTS custom_scope VARCHAR(255);
ALTER TABLE subagent_tasks ADD COLUMN IF NOT EXISTS custom_scope VARCHAR(255);

-- From 000044_seed_agents_core_task_files.up.sql
-- Seed AGENTS_CORE.md for all agents that have AGENTS.md but lack AGENTS_CORE.md
INSERT INTO agent_context_files (id, agent_id, file_name, content, tenant_id, created_at, updated_at)
SELECT gen_random_uuid(), a.id, 'AGENTS_CORE.md',
  E'# Operating Rules (Core)\n\n## Language & Communication\n\n- Match the user''s language \u2014 if user writes Vietnamese, reply in Vietnamese. Detect from first message, stay consistent.\n\n## Internal Messages\n\n- `[System Message]` blocks are internal context (cron results, subagent completions). Not user-visible.\n- If a system message reports completed work, rewrite in your normal voice and send. Don''t forward raw system text.\n- Never use `exec` or `curl` for messaging \u2014 GoClaw handles all routing internally.\n- When asked to save or remember something, you MUST call a write tool (`write_file` or `edit`) in THIS turn. Never claim \"already saved\" without a tool call.\n',
  a.tenant_id, NOW(), NOW()
FROM agents a
WHERE a.deleted_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM agent_context_files
    WHERE agent_id = a.id AND file_name = 'AGENTS_CORE.md'
  );

-- Seed AGENTS_TASK.md for all agents that have AGENTS.md but lack AGENTS_TASK.md
INSERT INTO agent_context_files (id, agent_id, file_name, content, tenant_id, created_at, updated_at)
SELECT gen_random_uuid(), a.id, 'AGENTS_TASK.md',
  E'# Operating Rules (Task)\n\n## Language & Communication\n\n- Match the user''s language \u2014 if user writes Vietnamese, reply in Vietnamese. Detect from first message, stay consistent.\n\n## Internal Messages\n\n- `[System Message]` blocks are internal context (cron results, subagent completions). Not user-visible.\n- If a system message reports completed work, rewrite in your normal voice and send. Don''t forward raw system text.\n- Never use `exec` or `curl` for messaging \u2014 GoClaw handles all routing internally.\n- When asked to save or remember something, you MUST call a write tool (`write_file` or `edit`) in THIS turn. Never claim \"already saved\" without a tool call.\n\n## Memory\n\n- **Recall:** Use `memory_search` before answering about prior work, decisions, or preferences\n- **Save:** Use `write_file` to persist important information:\n  - Daily notes -> `memory/YYYY-MM-DD.md`\n  - Long-term -> `MEMORY.md` (curated: key decisions, lessons, significant events)\n- **No \"mental notes\"** \u2014 if you want to remember something, write it to a file NOW\n- **Recall details:** Use `memory_search` first, then `memory_get` to pull only needed lines.\n  If `knowledge_graph_search` is available, also run it for multi-hop relationships.\n\n### MEMORY.md Privacy\n\n- Only reference MEMORY.md content in **private/direct chats** with your user\n- In group chats or shared sessions, do NOT surface personal memory content\n\n## Scheduling\n\nUse the `cron` tool for periodic or timed tasks.\n- Keep messages specific and actionable\n- Use `kind: \"at\"` for one-shot reminders (auto-deletes after running)\n- Use `deliver: true` with `channel` and `to` to send output to a chat\n- Don''t create too many frequent jobs \u2014 batch related checks\n',
  a.tenant_id, NOW(), NOW()
FROM agents a
WHERE a.deleted_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM agent_context_files
    WHERE agent_id = a.id AND file_name = 'AGENTS_TASK.md'
  );

-- Cleanup: remove AGENTS_MINIMAL.md entries (deprecated v1 remnant)
DELETE FROM agent_context_files WHERE file_name = 'AGENTS_MINIMAL.md';

-- From 000045_episodic_recall_tracking.up.sql
-- Phase 10: dreaming weighted scoring. Track per-episode recall signals
-- that feed ComputeRecallScore() in internal/consolidation/scoring.go.
-- recall_score stores the running-average of search hit scores so the
-- dreaming worker can sort unpromoted entries by perceived usefulness
-- instead of strictly oldest-first.
ALTER TABLE episodic_summaries ADD COLUMN IF NOT EXISTS recall_count INT DEFAULT 0 NOT NULL;
ALTER TABLE episodic_summaries ADD COLUMN IF NOT EXISTS recall_score DOUBLE PRECISION DEFAULT 0 NOT NULL;
ALTER TABLE episodic_summaries ADD COLUMN IF NOT EXISTS last_recalled_at TIMESTAMPTZ;

-- Partial index for DreamingWorker.ListUnpromotedScored — only touches
-- unpromoted rows and orders by recall_score DESC to match the primary
-- query shape: "top-N unpromoted with highest recall signal".
CREATE INDEX IF NOT EXISTS idx_episodic_recall_unpromoted
    ON episodic_summaries(agent_id, user_id, recall_score DESC)
    WHERE promoted_at IS NULL;

-- From 000046_vault_nullable_agent_id.up.sql
-- Make agent_id nullable so team-scoped and tenant-shared files can exist
-- without an owning agent.

-- 1. Drop NOT NULL on agent_id.
ALTER TABLE vault_documents ALTER COLUMN agent_id DROP NOT NULL;

-- 2. Change FK from CASCADE to SET NULL (agent deletion preserves docs).
ALTER TABLE vault_documents DROP CONSTRAINT vault_documents_agent_id_fkey;
ALTER TABLE vault_documents ADD CONSTRAINT vault_documents_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE SET NULL;

-- 3. Replace unique index: add tenant_id as leading column, COALESCE both nullable cols.
DROP INDEX IF EXISTS uq_vault_docs_agent_team_scope_path;
CREATE UNIQUE INDEX uq_vault_docs_agent_team_scope_path
    ON vault_documents (
        tenant_id,
        COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'),
        COALESCE(team_id, '00000000-0000-0000-0000-000000000000'),
        scope,
        path
    );

-- 4. Trigger: when agent deleted (SET NULL) and no team -> scope='shared'.
CREATE OR REPLACE FUNCTION vault_docs_agent_null_scope_fix()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.agent_id IS NULL AND OLD.agent_id IS NOT NULL AND NEW.team_id IS NULL THEN
        NEW.scope := 'shared';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_vault_docs_agent_null_scope
    BEFORE UPDATE OF agent_id ON vault_documents
    FOR EACH ROW
    EXECUTE FUNCTION vault_docs_agent_null_scope_fix();

-- 5. Partial index for agent-scoped queries (skip NULLs).
DROP INDEX IF EXISTS idx_vault_docs_agent_scope;
CREATE INDEX idx_vault_docs_agent_scope ON vault_documents(agent_id, scope) WHERE agent_id IS NOT NULL;

-- From 000047_cron_jobs_unique_constraint.up.sql
-- Deduplicate cron_jobs: keep most recently updated row per (agent_id, tenant_id, name).
DELETE FROM cron_jobs
WHERE id NOT IN (
  SELECT DISTINCT ON (agent_id, tenant_id, name) id
  FROM cron_jobs
  ORDER BY agent_id, tenant_id, name, updated_at DESC
);

-- Create unique index (will succeed after dedup).
CREATE UNIQUE INDEX IF NOT EXISTS uq_cron_jobs_agent_tenant_name
  ON cron_jobs (agent_id, tenant_id, name);

-- Generated column for path basename (case-insensitive wikilink resolution).
ALTER TABLE vault_documents ADD COLUMN IF NOT EXISTS path_basename TEXT
  GENERATED ALWAYS AS (lower(regexp_replace(path, '.+/', ''))) STORED;

-- Index for fast basename lookup by tenant.
CREATE INDEX IF NOT EXISTS idx_vault_docs_basename
  ON vault_documents(tenant_id, path_basename);

-- From 000048_vault_media_linking.up.sql
-- Phase 03: vault media linking foundation.
-- Adds base_name index on task attachments, metadata column on vault_links
-- for cleanup tracking, repairs missing CASCADE FKs on vault_links, and a
-- partial index for delegation lookup inside vault_documents.metadata.

-- 1. GENERATED base_name on team_task_attachments (mirrors vault_documents.path_basename).
ALTER TABLE team_task_attachments
  ADD COLUMN IF NOT EXISTS base_name TEXT
  GENERATED ALWAYS AS (lower(regexp_replace(path, '.+/', ''))) STORED;

CREATE INDEX IF NOT EXISTS idx_tta_tenant_basename
  ON team_task_attachments(tenant_id, base_name);

-- 2. metadata column on vault_links for cleanup tracking (task:{id}, delegation:{id}).
ALTER TABLE vault_links
  ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS idx_vault_links_source
  ON vault_links((metadata->>'source'))
  WHERE metadata ? 'source';

-- 3. Fix missing CASCADE on vault_links FKs (SQLite already has CASCADE at schema.sql:1548-1549).
ALTER TABLE vault_links DROP CONSTRAINT IF EXISTS vault_links_from_doc_id_fkey;
ALTER TABLE vault_links
  ADD CONSTRAINT vault_links_from_doc_id_fkey
  FOREIGN KEY (from_doc_id) REFERENCES vault_documents(id) ON DELETE CASCADE;

ALTER TABLE vault_links DROP CONSTRAINT IF EXISTS vault_links_to_doc_id_fkey;
ALTER TABLE vault_links
  ADD CONSTRAINT vault_links_to_doc_id_fkey
  FOREIGN KEY (to_doc_id) REFERENCES vault_documents(id) ON DELETE CASCADE;

-- 4. Partial index for delegation-id lookup in vault_documents.metadata.
--    Keeps index small — only rows explicitly tagged with a delegation_id.
CREATE INDEX IF NOT EXISTS idx_vault_docs_delegation
  ON vault_documents((metadata->>'delegation_id'))
  WHERE metadata ? 'delegation_id';

-- From 000049_vault_path_prefix_index.up.sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_vault_docs_path_prefix
    ON vault_documents (tenant_id, path text_pattern_ops);

-- From 000050_listen_raw_messages.up.sql
CREATE TABLE listen_raw_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_name VARCHAR(100) NOT NULL,
    chat_id VARCHAR(255) NOT NULL,
    chat_name VARCHAR(255) NOT NULL DEFAULT '',
    graph_id VARCHAR(255) NOT NULL,
    sender VARCHAR(255) NOT NULL DEFAULT '',
    sender_id VARCHAR(255) NOT NULL DEFAULT '',
    body TEXT NOT NULL DEFAULT '',
    msg_timestamp TIMESTAMPTZ NOT NULL,
    agent_id UUID NOT NULL,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ
);

CREATE INDEX idx_listen_raw_agent_chat ON listen_raw_messages(agent_id, chat_id, created_at);
CREATE INDEX idx_listen_raw_pending ON listen_raw_messages(processed_at) WHERE processed_at IS NULL;
CREATE INDEX idx_listen_raw_tenant ON listen_raw_messages(tenant_id);

-- From 000051_span_system_prompt_preview.up.sql
ALTER TABLE spans ADD COLUMN system_prompt_preview TEXT;

-- From 000052_kg_entity_event_time.up.sql
ALTER TABLE kg_entities ADD COLUMN IF NOT EXISTS event_time TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_kg_entities_event_time
    ON kg_entities(agent_id, user_id, event_time) WHERE event_time IS NOT NULL;

-- From 000053_listen_raw_media.up.sql
-- Add media_refs column to listen_raw_messages for storing media attachment references.
-- Each raw message can have 0-N media attachments persisted alongside the text body.
ALTER TABLE listen_raw_messages ADD COLUMN media_refs JSONB NOT NULL DEFAULT '[]';

-- From 000054_seed_builtin_tools_stt.up.sql
-- Seed the STT builtin_tools row. ON CONFLICT preserves user-customized settings.
INSERT INTO builtin_tools (name, display_name, description, category, enabled, settings)
VALUES ('stt', 'Speech-to-Text', 'Transcribe voice/audio messages to text using ElevenLabs Scribe or a proxy service', 'media', true, '{}')
ON CONFLICT (name) DO NOTHING;

-- From 000055_backfill_context_pruning_mode.up.sql
-- Backfill mode: "cache-ttl" for agents that have custom context_pruning config
-- but are missing the "mode" field. This preserves user intent after the
-- opt-in default flip (faithful port of TS behavior) — see CHANGELOG.
--
-- Rows matched: context_pruning is a non-empty JSON object without a "mode" key.
-- Rows skipped: NULL, empty object, non-object values, or already has "mode".
UPDATE agents
SET context_pruning = jsonb_set(context_pruning, '{mode}', '"cache-ttl"'::jsonb)
WHERE context_pruning IS NOT NULL
  AND jsonb_typeof(context_pruning) = 'object'
  AND context_pruning <> '{}'::jsonb
  AND NOT (context_pruning ? 'mode');

-- From 000056_agent_hooks.up.sql
-- Migration 000052: Agent hooks system
-- Creates agent_hooks, hook_executions, and tenant_hook_budget tables.
-- scope uses CHECK-based enum (no separate ENUM type) for portability.
-- Global-scope hooks use the MasterTenantID (0193a5b0-7000-7000-8000-000000000001)
-- as tenant_id. This aligns with store.MasterTenantID / store.IsMasterScope
-- conventions and satisfies any future FK to tenants(id).

-- ============================================================
-- Table: agent_hooks
-- ============================================================

CREATE TABLE IF NOT EXISTS agent_hooks (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001',
    agent_id     UUID REFERENCES agents(id) ON DELETE CASCADE,
    scope        VARCHAR(8) NOT NULL CHECK (scope IN ('global', 'tenant', 'agent')),
    event        VARCHAR(32) NOT NULL,
    handler_type VARCHAR(16) NOT NULL CHECK (handler_type IN ('command', 'http', 'prompt')),
    -- config holds handler-specific options (e.g. command path, http url, prompt template).
    config       JSONB NOT NULL DEFAULT '{}',
    -- matcher is an optional regex applied to tool_name before the hook fires.
    matcher      VARCHAR(256),
    -- if_expr is an optional CEL expression evaluated against tool_input.
    if_expr      TEXT,
    timeout_ms   INT NOT NULL DEFAULT 5000,
    on_timeout   VARCHAR(8) NOT NULL DEFAULT 'block' CHECK (on_timeout IN ('block', 'allow')),
    priority     INT NOT NULL DEFAULT 0,
    enabled      BOOL NOT NULL DEFAULT TRUE,
    version      INT NOT NULL DEFAULT 1,
    -- source: 'ui' (dashboard), 'api', 'seed' (bootstrap).
    source       VARCHAR(8) NOT NULL DEFAULT 'ui' CHECK (source IN ('ui', 'api', 'seed')),
    -- metadata stores UI-only fields (tags, notes, lastTestedAt, createdByUsername)
    -- and is extensible without future migrations. Project convention: always present.
    metadata     JSONB NOT NULL DEFAULT '{}',
    created_by   UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Partial unique indexes per scope prevent duplicate hooks within the same scope
-- while allowing the same (event, handler_type) across different scopes (C3 fix).
CREATE UNIQUE INDEX IF NOT EXISTS uq_hooks_global
    ON agent_hooks (event, handler_type)
    WHERE scope = 'global';

CREATE UNIQUE INDEX IF NOT EXISTS uq_hooks_tenant
    ON agent_hooks (tenant_id, event, handler_type)
    WHERE scope = 'tenant';

CREATE UNIQUE INDEX IF NOT EXISTS uq_hooks_agent
    ON agent_hooks (tenant_id, agent_id, event, handler_type)
    WHERE scope = 'agent';

-- idx_hooks_lookup: hot-path index for ResolveForEvent queries.
-- Partial WHERE enabled reduces index size (most production rows will be enabled).
CREATE INDEX IF NOT EXISTS idx_hooks_lookup
    ON agent_hooks (tenant_id, agent_id, event)
    WHERE enabled = TRUE;

-- ============================================================
-- Table: hook_executions (append-only audit log)
-- ============================================================

CREATE TABLE IF NOT EXISTS hook_executions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- hook_id is SET NULL when the parent hook is deleted (preserves audit trail).
    hook_id     UUID REFERENCES agent_hooks(id) ON DELETE SET NULL,
    session_id  VARCHAR(500),
    event       VARCHAR(32) NOT NULL,
    -- input_hash is canonical-JSON sha256 (64 hex chars) of (tool_name + sorted args).
    input_hash  CHAR(64),
    decision    VARCHAR(16) NOT NULL CHECK (decision IN ('allow', 'block', 'error', 'timeout')),
    duration_ms INT NOT NULL DEFAULT 0,
    retry       INT NOT NULL DEFAULT 0,
    -- dedup_key prevents duplicate execution rows for the same (hook_id, event_id).
    dedup_key   VARCHAR(128),
    -- error truncated to 256 chars at write time (M2 mitigation).
    error       VARCHAR(256),
    -- error_detail stores the full error AES-256-GCM encrypted; GDPR-purgeable.
    error_detail BYTEA,
    -- metadata stores extensible exec context: matcher_matched, cel_eval_result,
    -- stdout_len, http_status, prompt_model, prompt_tokens, trace_id. Avoids
    -- future migration for new observability fields.
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_hook_executions_session
    ON hook_executions (session_id, created_at);

CREATE UNIQUE INDEX IF NOT EXISTS uq_hook_executions_dedup
    ON hook_executions (dedup_key)
    WHERE dedup_key IS NOT NULL;

-- ============================================================
-- Table: tenant_hook_budget
-- Consolidated here (Phase 3 prompt handler atomic deduct).
-- One row per tenant tracks monthly spend against a cap.
-- ============================================================

CREATE TABLE IF NOT EXISTS tenant_hook_budget (
    tenant_id    UUID PRIMARY KEY,
    month_start  DATE NOT NULL,
    budget_total BIGINT NOT NULL DEFAULT 0,
    remaining    BIGINT NOT NULL DEFAULT 0,
    last_warned_at TIMESTAMPTZ,
    -- metadata stores alert thresholds, override flags, notes.
    metadata     JSONB NOT NULL DEFAULT '{}',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- From 000057_agent_hooks_script_and_builtin.up.sql
-- 000053 — Relax CHECK constraints for script handler + builtin source.
--
-- Phase 03 of the Wave 1 hooks plan. Before this migration the hook system
-- only accepts command/http/prompt handlers and ui/api/seed sources. Phase 02
-- landed a goja-backed script handler; Phase 04 ships builtin hooks whose
-- source marker must be persisted as 'builtin'. Uniqueness indexes on
-- (event, handler_type) per scope are also dropped — scripts routinely want
-- many small hooks per event (e.g. a redactor per PII type).

ALTER TABLE agent_hooks DROP CONSTRAINT IF EXISTS agent_hooks_handler_type_check;
ALTER TABLE agent_hooks ADD CONSTRAINT agent_hooks_handler_type_check
  CHECK (handler_type IN ('command', 'http', 'prompt', 'script'));

ALTER TABLE agent_hooks ALTER COLUMN source TYPE VARCHAR(16);
ALTER TABLE agent_hooks DROP CONSTRAINT IF EXISTS agent_hooks_source_check;
ALTER TABLE agent_hooks ADD CONSTRAINT agent_hooks_source_check
  CHECK (source IN ('ui', 'api', 'seed', 'builtin'));

DROP INDEX IF EXISTS uq_hooks_global;
DROP INDEX IF EXISTS uq_hooks_tenant;
DROP INDEX IF EXISTS uq_hooks_agent;

-- From 000058_agent_hooks_name.up.sql
-- 000054 — Add `name` column, N:M junction, rename tables, drop agent_id.
--
-- 1) name: user-facing label. Nullable so existing rows don't break.
-- 2) Junction table replaces the 1:N agent_id FK with many-to-many.
-- 3) Data migration: copy existing agent_id values into junction.
-- 4) Rename agent_hooks → hooks, agent_hook_agents → hook_agents.
-- 5) Drop deprecated agent_id column from hooks.
-- 6) Recreate indexes with new table names.

-- Step 1: add name column
ALTER TABLE agent_hooks ADD COLUMN IF NOT EXISTS name VARCHAR(255);

-- Step 2: create junction table
CREATE TABLE IF NOT EXISTS agent_hook_agents (
    hook_id  UUID NOT NULL REFERENCES agent_hooks(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    PRIMARY KEY (hook_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_hook_agents_agent
    ON agent_hook_agents (agent_id);

-- Step 3: copy existing agent_id values into junction
INSERT INTO agent_hook_agents (hook_id, agent_id)
SELECT id, agent_id FROM agent_hooks
WHERE agent_id IS NOT NULL
ON CONFLICT DO NOTHING;

-- Step 4: rename tables
ALTER TABLE agent_hooks RENAME TO hooks;
ALTER TABLE agent_hook_agents RENAME TO hook_agents;

-- Step 5: drop deprecated column
ALTER TABLE hooks DROP COLUMN IF EXISTS agent_id;

-- Step 6: recreate indexes with new table/column names
DROP INDEX IF EXISTS idx_hooks_lookup;
CREATE INDEX idx_hooks_lookup ON hooks (tenant_id, event) WHERE enabled = TRUE;

DROP INDEX IF EXISTS idx_hook_agents_agent;
CREATE INDEX idx_hook_agents_agent ON hook_agents (agent_id);

-- From 000059_vault_scope_consistency_check.up.sql
-- Scope/ownership invariant for vault_documents.
-- personal → agent_id NOT NULL, team_id NULL
-- team     → team_id NOT NULL, agent_id NULL
-- shared   → both NULL
-- custom   → no constraint (user-defined scopes)
ALTER TABLE vault_documents
    ADD CONSTRAINT vault_documents_scope_consistency
    CHECK (
        (scope = 'personal' AND agent_id IS NOT NULL AND team_id IS NULL) OR
        (scope = 'team'     AND team_id  IS NOT NULL AND agent_id IS NULL) OR
        (scope = 'shared'   AND agent_id IS NULL     AND team_id  IS NULL) OR
        scope = 'custom'
    ) NOT VALID;

-- Ops step (run after audit cleanup):
--   ALTER TABLE vault_documents VALIDATE CONSTRAINT vault_documents_scope_consistency;

-- From 000060_vault_chat_id.up.sql
-- Add chat_id to vault_documents for cross-chat isolation within isolated teams.
-- NULL = team-wide doc (shared mode or legacy); non-NULL = scoped to specific chat.
ALTER TABLE vault_documents ADD COLUMN IF NOT EXISTS chat_id TEXT;

-- Composite index for team + chat filtering (primary query pattern for isolated teams).
CREATE INDEX IF NOT EXISTS idx_vault_docs_team_chat
    ON vault_documents(team_id, chat_id)
    WHERE team_id IS NOT NULL;

-- Drop scope-consistency CHECK before backfill UPDATEs. Constraint was added
-- NOT VALID in migration 55, which skips existing rows but still re-checks
-- on every UPDATE. Legacy data (pre-M46 when agent_id was NOT NULL, pre-M43
-- before team_id existed) often has rows that violate the invariant, causing
-- the backfill UPDATEs below to abort and leave migration 56 in a dirty
-- state (issue #1035). Drop now, re-add at end so fresh installs proceed
-- cleanly. Constraint stays NOT VALID — legacy bad rows still tolerated;
-- a future migration can clean + VALIDATE.
ALTER TABLE vault_documents DROP CONSTRAINT IF EXISTS vault_documents_scope_consistency;

-- -----------------------------------------------------------------------------
-- Backfill 1: team-scoped docs (scope='team', team_id set).
-- Two path layouts:
--   master tenant:     teams/<team_uuid>/<chat>/...
--   non-master tenant: tenants/<slug>/teams/<team_uuid>/<chat>/...
-- Chat segments starting with '.' (e.g. '.goclaw') are config dirs, not real chats — skip.
-- -----------------------------------------------------------------------------
UPDATE vault_documents vd
SET chat_id = (regexp_match(vd.path, '^(?:tenants/[^/]+/)?teams/[^/]+/([^/]+)/'))[1]
FROM agent_teams t
WHERE vd.team_id = t.id
  AND (t.settings->>'workspace_scope' IS NULL OR t.settings->>'workspace_scope' != 'shared')
  AND vd.path ~ '^(?:tenants/[^/]+/)?teams/[^/]+/[^.][^/]*/';

-- -----------------------------------------------------------------------------
-- Backfill 2: legacy docs from before team scope (team_id IS NULL) with chat
-- identifiers embedded in their path. Without chat_id these leak across chats
-- in isolated-team search because the `searchChatFilter` predicate cannot
-- distinguish them.
--
-- Subsystem vocabulary — any channel integration or delivery surface the
-- gateway writes under. Must match the channel names used as path segments
-- in internal/channels/* and workspace resolver (v2 + v3 layouts).
--   Channels:  telegram | discord | zalo | feishu | lark | whatsapp | slack |
--              line | messenger | wechat | viber
--   Transports: ws (browser / WS direct) | api (HTTP) | delegate (subagent)
--
-- Path layouts handled (in order of COALESCE priority):
--   <subsystem>/group_<anything>_<chat>/...              (Telegram-style group prefix)
--   <subsystem>/<chat>/...                               (bare legacy)
--   <agent_key>/<subsystem>/group_<anything>_<chat>/...  (agent-owned group)
--   <agent_key>/<subsystem>/<chat>/...                   (agent-owned direct)
--   tenants/<slug>/<subsystem>/<chat>/...                (non-master tenant)
--   <agent_key>/<botname>/group_<botname>_<chat>/...     (legacy bot channel)
--   <agent_key>/<botname>/<chat>/...                     (bot + numeric/ws chat)
--
-- Chat IDs: numeric (Telegram/Discord/Zalo), oc_xxx (Feishu/Lark), sanitized
-- JID (WhatsApp: "123_c_us"), `system`, user handles, UUIDs. Sanitizer
-- (workspace_resolver.go) replaces everything outside [a-zA-Z0-9_-] with `_`,
-- so the captured character class matches what's actually on disk.
--
-- Only populate when chat_id IS NULL so interceptor-stamped values survive.
-- -----------------------------------------------------------------------------
UPDATE vault_documents
SET chat_id = COALESCE(
    (regexp_match(path, '^(?:telegram|discord|zalo|feishu|lark|whatsapp|slack|line|messenger|wechat|viber)/group_[^/]+_(-?[0-9]+)/'))[1],
    (regexp_match(path, '^(?:telegram|discord|zalo|feishu|lark|whatsapp|slack|line|messenger|wechat|viber|ws|delegate|api)/([a-zA-Z0-9_-]+)/'))[1],
    (regexp_match(path, '^[^/]+/(?:telegram|discord|zalo|feishu|lark|whatsapp|slack|line|messenger|wechat|viber)/group_[^/]+_(-?[0-9]+)/'))[1],
    (regexp_match(path, '^[^/]+/(?:telegram|discord|zalo|feishu|lark|whatsapp|slack|line|messenger|wechat|viber|ws|delegate|api)/([a-zA-Z0-9_-]+)/'))[1],
    (regexp_match(path, '^tenants/[^/]+/(?:telegram|discord|zalo|feishu|lark|whatsapp|slack|line|messenger|wechat|viber|ws|delegate|api)/([a-zA-Z0-9_-]+)/'))[1],
    (regexp_match(path, '^group_[^/]+_(-?[0-9]+)/'))[1],
    (regexp_match(path, '^[^/]+/[^/]+/group_[^/]+_(-?[0-9]+)/'))[1],
    (regexp_match(path, '^[^/]+/[^/]+/([a-zA-Z0-9_-]+)/'))[1]
)
WHERE chat_id IS NULL
  AND team_id IS NULL
  AND (
    path ~ '^(?:[^/]+/)?(?:telegram|discord|zalo|feishu|lark|whatsapp|slack|line|messenger|wechat|viber|ws|delegate|api)/[^/]+/'
    OR path ~ '^tenants/[^/]+/(?:telegram|discord|zalo|feishu|lark|whatsapp|slack|line|messenger|wechat|viber|ws|delegate|api)/[^/]+/'
    OR path ~ '^group_[^/]+_-?[0-9]+/'
    OR path ~ '^[^/]+/[^/]+/(group_[^/]+_-?[0-9]+|[0-9]+)/'
  );

-- Re-add the scope-consistency constraint dropped above. NOT VALID matches
-- migration 55's original semantics — existing rows pass without validation,
-- new INSERT/UPDATE are checked. Run `VALIDATE CONSTRAINT` after audit cleanup.
ALTER TABLE vault_documents
    ADD CONSTRAINT vault_documents_scope_consistency
    CHECK (
        (scope = 'personal' AND agent_id IS NOT NULL AND team_id IS NULL) OR
        (scope = 'team'     AND team_id  IS NOT NULL AND agent_id IS NULL) OR
        (scope = 'shared'   AND agent_id IS NULL     AND team_id  IS NULL) OR
        scope = 'custom'
    ) NOT VALID;

-- From 000061_mcp_health_checks.up.sql
-- MCP health check history: records every health ping result for each server.
CREATE TABLE mcp_health_checks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id UUID REFERENCES mcp_servers(id) ON DELETE CASCADE,
    server_name VARCHAR(255) NOT NULL,
    tenant_id UUID NOT NULL,
    status VARCHAR(20) NOT NULL,      -- "healthy", "unhealthy", "reconnecting"
    latency_ms INTEGER,               -- ping round-trip (null for unhealthy)
    error TEXT,                        -- error message (null for healthy)
    tool_count INTEGER DEFAULT 0,
    health_failures INTEGER DEFAULT 0,
    checked_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_mcp_health_server_time ON mcp_health_checks (server_id, checked_at DESC);
CREATE INDEX idx_mcp_health_tenant_time ON mcp_health_checks (tenant_id, checked_at DESC);

-- From 000062_listen_raw_extraction_status.up.sql
-- Add extraction tracking columns to listen_raw_messages.
-- extraction_status tracks the per-message extraction lifecycle.
-- Values: 'pending' (default), 'extracted', 'failed'.
ALTER TABLE listen_raw_messages ADD COLUMN extraction_status VARCHAR(20) NOT NULL DEFAULT 'pending';
ALTER TABLE listen_raw_messages ADD COLUMN extraction_error TEXT;
ALTER TABLE listen_raw_messages ADD COLUMN extraction_attempts INT NOT NULL DEFAULT 0;
ALTER TABLE listen_raw_messages ADD COLUMN last_attempted_at TIMESTAMPTZ;

-- Index for filtering failed extractions in the UI.
CREATE INDEX idx_listen_raw_extraction_status ON listen_raw_messages(extraction_status)
    WHERE extraction_status = 'failed';

-- Backfill: existing processed rows are 'extracted'.
UPDATE listen_raw_messages SET extraction_status = 'extracted' WHERE processed_at IS NOT NULL;

-- From 000064_raw_message_chunks.up.sql
-- Raw message chunks: chunked+embedded raw messages for vector search.
CREATE TABLE raw_message_chunks (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id      UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    graph_id      VARCHAR(255) NOT NULL DEFAULT '',
    chat_id       VARCHAR(255) NOT NULL DEFAULT '',
    chat_name     VARCHAR(255) NOT NULL DEFAULT '',
    sender        VARCHAR(255) NOT NULL DEFAULT '',
    sender_id     VARCHAR(255) NOT NULL DEFAULT '',
    msg_time_from TIMESTAMPTZ NOT NULL,
    msg_time_to   TIMESTAMPTZ NOT NULL,
    chunk_index   INT NOT NULL DEFAULT 0,
    text          TEXT NOT NULL,
    content_hash  VARCHAR(64) NOT NULL,
    embedding     vector(1536),
    tsv           tsvector GENERATED ALWAYS AS (to_tsvector('simple', text)) STORED,
    source_msg_ids UUID[] NOT NULL DEFAULT '{}',
    tenant_id     UUID NOT NULL REFERENCES tenants(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_rmc_agent_graph ON raw_message_chunks(agent_id, graph_id);
CREATE INDEX idx_rmc_agent_chat  ON raw_message_chunks(agent_id, chat_id);
CREATE INDEX idx_rmc_tsv         ON raw_message_chunks USING GIN(tsv);
CREATE INDEX idx_rmc_tenant      ON raw_message_chunks(tenant_id);
CREATE INDEX idx_rmc_embedding   ON raw_message_chunks USING hnsw(embedding vector_cosine_ops);

-- Separate tracking for embedding processing (KG extraction uses processed_at).
ALTER TABLE listen_raw_messages ADD COLUMN embedded_at TIMESTAMPTZ;
CREATE INDEX idx_listen_raw_embed_pending ON listen_raw_messages(embedded_at) WHERE embedded_at IS NULL;

-- From 000065_vector_dimensions_768.up.sql
-- Resize all vector columns from 1536 to 768 dimensions to match
-- the embeddinggemma-300m model output.

-- Drop HNSW indexes first (cannot ALTER column with dependent indexes)
DROP INDEX IF EXISTS idx_mem_vec;
DROP INDEX IF EXISTS idx_agents_embedding;
DROP INDEX IF EXISTS idx_episodic_vec;
DROP INDEX IF EXISTS idx_episodic_embedding_hnsw;
DROP INDEX IF EXISTS idx_kg_entity_vec;
DROP INDEX IF EXISTS idx_skills_embedding;
DROP INDEX IF EXISTS idx_tt_embedding;
DROP INDEX IF EXISTS idx_vault_docs_embedding;
DROP INDEX IF EXISTS idx_rmc_embedding; -- already 768 but drop for clean recreate

-- Clear cached embeddings (will be regenerated with correct dimensions)
DELETE FROM embedding_cache WHERE embedding IS NOT NULL;

-- Alter all vector columns
ALTER TABLE memory_chunks       ALTER COLUMN embedding TYPE vector(768);
ALTER TABLE agents              ALTER COLUMN embedding TYPE vector(768);
ALTER TABLE episodic_summaries  ALTER COLUMN embedding TYPE vector(768);
ALTER TABLE kg_entities         ALTER COLUMN embedding TYPE vector(768);
ALTER TABLE skills              ALTER COLUMN embedding TYPE vector(768);
ALTER TABLE team_tasks          ALTER COLUMN embedding TYPE vector(768);
ALTER TABLE vault_documents     ALTER COLUMN embedding TYPE vector(768);
ALTER TABLE embedding_cache     ALTER COLUMN embedding TYPE vector(768);
-- raw_message_chunks already vector(768) from manual ALTER, include for idempotency
ALTER TABLE raw_message_chunks  ALTER COLUMN embedding TYPE vector(768);

-- Recreate HNSW indexes
CREATE INDEX idx_mem_vec ON memory_chunks USING hnsw(embedding vector_cosine_ops);
CREATE INDEX idx_agents_embedding ON agents USING hnsw(embedding vector_cosine_ops);
CREATE INDEX idx_episodic_vec ON episodic_summaries USING hnsw(embedding vector_cosine_ops);
CREATE INDEX idx_episodic_embedding_hnsw ON episodic_summaries USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64) WHERE embedding IS NOT NULL;
CREATE INDEX idx_kg_entity_vec ON kg_entities USING hnsw(embedding vector_cosine_ops);
CREATE INDEX idx_skills_embedding ON skills USING hnsw(embedding vector_cosine_ops);
CREATE INDEX idx_tt_embedding ON team_tasks USING hnsw(embedding vector_cosine_ops);
CREATE INDEX idx_vault_docs_embedding ON vault_documents USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64);
CREATE INDEX idx_rmc_embedding ON raw_message_chunks USING hnsw(embedding vector_cosine_ops);

-- From 000066_raw_msg_chunks_text_columns.up.sql
-- Widen sender column from VARCHAR(255) to TEXT.
-- With evaluated chunk params (27852 chars), more messages per chunk means
-- more unique senders aggregated by uniqueSenders(), exceeding VARCHAR(255).
ALTER TABLE raw_message_chunks ALTER COLUMN sender TYPE TEXT;

-- From 000067_embedded_chunks.up.sql
ALTER TABLE usage_snapshots ADD COLUMN IF NOT EXISTS embedded_chunks INTEGER NOT NULL DEFAULT 0;

-- From 000068_add_openrouter_routing.up.sql
-- No-op: OpenRouter routing moved to provider-level settings (llm_providers.settings.openrouter_routing).

-- From 000069_agent_grants_env_override.up.sql
-- Add optional per-grant env override for secure CLI agent grants.
-- NULL = no grant-level override; binary-level env is used instead.
-- Mirrors secure_cli_user_credentials.encrypted_env AES-256-GCM pattern.
ALTER TABLE secure_cli_agent_grants ADD COLUMN encrypted_env BYTEA;

-- From 000070_webhooks.up.sql
-- Webhook registry + call audit log.
-- tenant_id on every row — all queries must include WHERE tenant_id = $N.
-- secret_hash stores SHA-256 hex; raw secret returned only once on create (phase-04).

-- ============================================================
-- Table: webhooks  (registry)
-- ============================================================
CREATE TABLE webhooks (
    id                  uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid        NOT NULL,
    agent_id            uuid        REFERENCES agents(id) ON DELETE SET NULL,
    name                text        NOT NULL,
    kind                text        NOT NULL CHECK (kind IN ('llm', 'message')),
    secret_prefix       text,
    secret_hash         text        NOT NULL,
    scopes              text[]      NOT NULL DEFAULT '{}',
    channel_id          uuid,
    rate_limit_per_min  int         NOT NULL DEFAULT 60,
    ip_allowlist        text[]      NOT NULL DEFAULT '{}',
    require_hmac        boolean     NOT NULL DEFAULT false,
    localhost_only      boolean     NOT NULL DEFAULT false,
    revoked             boolean     NOT NULL DEFAULT false,
    created_by          text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    last_used_at        timestamptz
);

CREATE INDEX idx_webhooks_tenant          ON webhooks (tenant_id);
CREATE INDEX idx_webhooks_tenant_agent    ON webhooks (tenant_id, agent_id);
CREATE UNIQUE INDEX uq_webhooks_secret    ON webhooks (secret_hash) WHERE revoked = false;

-- ============================================================
-- Table: webhook_calls  (audit + async state)
-- ============================================================
CREATE TABLE webhook_calls (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid        NOT NULL,
    webhook_id       uuid        NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    agent_id         uuid,
    idempotency_key  text,
    mode             text        NOT NULL CHECK (mode IN ('sync', 'async')),
    callback_url     text,
    status           text        NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'failed', 'dead')),
    attempts         int         NOT NULL DEFAULT 0,
    delivery_id      uuid        NOT NULL DEFAULT gen_random_uuid(),
    next_attempt_at  timestamptz,
    started_at       timestamptz,
    request_payload  jsonb,
    response         jsonb,
    last_error       text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    completed_at     timestamptz
);

CREATE INDEX idx_webhook_calls_tenant_created   ON webhook_calls (tenant_id, created_at DESC);
CREATE INDEX idx_webhook_calls_status_attempt   ON webhook_calls (status, next_attempt_at);
CREATE UNIQUE INDEX uq_webhook_calls_idempotency
    ON webhook_calls (webhook_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- From 000071_webhook_calls_lease_token.up.sql
-- K5: add lease_token to webhook_calls for optimistic-concurrency CAS.
-- ClaimNext sets lease_token = new UUID; UpdateStatus/MarkFailed guard with AND lease_token = $N.
-- ReclaimStale rotates lease_token to NULL so any in-flight CAS fails on next attempt.
ALTER TABLE webhook_calls ADD COLUMN lease_token TEXT;

-- From 000072_webhooks_encrypted_secret.up.sql
-- K6: store raw webhook secret encrypted at rest (AES-256-GCM via GOCLAW_ENCRYPTION_KEY).
-- encrypted_secret holds crypto.Encrypt(raw_secret, encKey) — never the raw bytes.
-- secret_hash is retained for bearer-token lookup (globally unique index).
-- HMAC signing uses decrypted encrypted_secret (raw bytes), not hex(secret_hash).
-- Existing webhooks (feature not shipped to prod) have encrypted_secret = '' → require rotation.
ALTER TABLE webhooks ADD COLUMN encrypted_secret TEXT NOT NULL DEFAULT '';

-- From 000073_workstations.up.sql
CREATE TABLE IF NOT EXISTS workstations (
    id              UUID PRIMARY KEY,
    workstation_key VARCHAR(100) NOT NULL,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name            VARCHAR(255) NOT NULL,
    backend_type    VARCHAR(20) NOT NULL CHECK (backend_type IN ('ssh','docker')),
    metadata        BYTEA NOT NULL,
    default_cwd     VARCHAR(500) NOT NULL DEFAULT '',
    default_env     BYTEA NOT NULL,
    active          BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by      VARCHAR(255) NOT NULL DEFAULT '',
    UNIQUE (tenant_id, workstation_key)
);
CREATE INDEX IF NOT EXISTS idx_workstations_tenant_active
    ON workstations(tenant_id, active) WHERE active = TRUE;

CREATE TABLE IF NOT EXISTS agent_workstation_links (
    agent_id        UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    workstation_id  UUID NOT NULL REFERENCES workstations(id) ON DELETE CASCADE,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    is_default      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (agent_id, workstation_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_workstation_default
    ON agent_workstation_links(agent_id) WHERE is_default = TRUE;
CREATE INDEX IF NOT EXISTS idx_agent_workstation_tenant ON agent_workstation_links(tenant_id);

-- From 000074_workstation_permissions.up.sql
-- Migration 000057: workstation_permissions (allowlist per workstation).
-- Default-deny: no matching enabled pattern → deny.
-- Pattern matches against argv[0] binary name only (not full command string).
-- Seeding happens inside WorkstationStore.Create transaction (H5 fix).

CREATE TABLE IF NOT EXISTS workstation_permissions (
    id             UUID PRIMARY KEY,
    workstation_id UUID NOT NULL REFERENCES workstations(id) ON DELETE CASCADE,
    tenant_id      UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    pattern        VARCHAR(500) NOT NULL,  -- binary name or prefix-glob, e.g. "git", "python*"
    enabled        BOOLEAN NOT NULL DEFAULT TRUE,
    created_by     VARCHAR(255) NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workstation_id, pattern)
);

-- Partial index: only index enabled entries (used in PermissionChecker.loadAllowlist).
CREATE INDEX idx_workstation_perms_ws ON workstation_permissions(workstation_id) WHERE enabled = TRUE;
CREATE INDEX idx_workstation_perms_tenant ON workstation_permissions(tenant_id);

-- From 000075_workstation_activity.up.sql
-- Migration 000058: workstation_activity — rolling audit log for exec events.
-- Append-only; pruned nightly via Prune(before) store method.
-- cmd_preview: first 200 chars of command (redacted secrets); cmd_hash: sha256 for forensics.

CREATE TABLE IF NOT EXISTS workstation_activity (
    id             UUID PRIMARY KEY,
    tenant_id      UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    workstation_id UUID NOT NULL REFERENCES workstations(id) ON DELETE CASCADE,
    agent_id       VARCHAR(255) NOT NULL DEFAULT '',
    action         VARCHAR(20)  NOT NULL,  -- 'exec' | 'deny'
    cmd_hash       VARCHAR(64)  NOT NULL DEFAULT '',
    cmd_preview    VARCHAR(200) NOT NULL DEFAULT '',
    exit_code      INTEGER,
    duration_ms    INTEGER,
    deny_reason    VARCHAR(200) NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ws_activity_ws_time     ON workstation_activity(workstation_id, created_at DESC);
CREATE INDEX idx_ws_activity_tenant_time ON workstation_activity(tenant_id, created_at DESC);
CREATE INDEX idx_ws_activity_retention   ON workstation_activity(created_at);

-- From 000076_workstation_command_groups.up.sql
-- ============================================================
-- Table: workstation_command_groups (migration 000076)
-- Named collections of command patterns that can be linked to
-- workstations. tenant_id IS NULL = global built-in; scoped
-- = tenant-created custom group.
-- ============================================================

CREATE TABLE workstation_command_groups (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID REFERENCES tenants(id) ON DELETE CASCADE,
    name        VARCHAR(100) NOT NULL,
    description TEXT,
    patterns    JSONB NOT NULL DEFAULT '[]',
    is_builtin  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  VARCHAR(100)
);

CREATE INDEX idx_workstation_cmd_groups_tenant ON workstation_command_groups(tenant_id);

-- ============================================================
-- Table: workstation_group_permissions
-- Junction linking command groups to workstations.
-- Enables/disables a group link independently.
-- ============================================================

CREATE TABLE workstation_group_permissions (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workstation_id UUID NOT NULL REFERENCES workstations(id) ON DELETE CASCADE,
    group_id       UUID NOT NULL REFERENCES workstation_command_groups(id) ON DELETE CASCADE,
    tenant_id      UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    enabled        BOOLEAN NOT NULL DEFAULT TRUE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workstation_id, group_id)
);

CREATE INDEX idx_workstation_group_perms_ws    ON workstation_group_permissions(workstation_id);
CREATE INDEX idx_workstation_group_perms_group ON workstation_group_permissions(group_id);

-- ============================================================
-- Seed built-in command groups (global, tenant_id IS NULL)
-- ============================================================

INSERT INTO workstation_command_groups (id, name, description, patterns, is_builtin, created_by) VALUES
  (gen_random_uuid(), 'Linux Monitoring', 'Common system monitoring commands', '["ps","top","free","df","htop","lsblk","vmstat","iostat","netstat","ss"]'::jsonb, TRUE, 'system'),
  (gen_random_uuid(), 'Container Tools',    'Docker and Kubernetes utilities', '["docker","kubectl","k9s","helm","ctr","nerdctl"]'::jsonb, TRUE, 'system'),
  (gen_random_uuid(), 'System Utilities',   'Service and log management',      '["systemctl","journalctl","service","timedatectl","hostnamectl"]'::jsonb, TRUE, 'system'),
  (gen_random_uuid(), 'Network Tools',      'Network diagnostics',             '["ping","traceroute","curl","wget","nslookup","dig","ss","ip"]'::jsonb, TRUE, 'system');

-- From 000077_seed_command_groups.up.sql
-- Seed built-in command groups idempotently.
-- Safe to re-run: each INSERT only executes if no group with that name exists.

INSERT INTO workstation_command_groups (id, tenant_id, name, description, patterns, is_builtin, created_by)
SELECT gen_random_uuid(), NULL, 'Linux Monitoring', 'Common system monitoring commands',
       '["ps","top","free","df","htop","lsblk","vmstat","iostat","netstat","ss"]'::jsonb, TRUE, 'system'
WHERE NOT EXISTS (SELECT 1 FROM workstation_command_groups WHERE name = 'Linux Monitoring');

INSERT INTO workstation_command_groups (id, tenant_id, name, description, patterns, is_builtin, created_by)
SELECT gen_random_uuid(), NULL, 'Container Tools', 'Docker and Kubernetes utilities',
       '["docker","kubectl","k9s","helm","ctr","nerdctl"]'::jsonb, TRUE, 'system'
WHERE NOT EXISTS (SELECT 1 FROM workstation_command_groups WHERE name = 'Container Tools');

INSERT INTO workstation_command_groups (id, tenant_id, name, description, patterns, is_builtin, created_by)
SELECT gen_random_uuid(), NULL, 'System Utilities', 'Service and log management',
       '["systemctl","journalctl","service","timedatectl","hostnamectl"]'::jsonb, TRUE, 'system'
WHERE NOT EXISTS (SELECT 1 FROM workstation_command_groups WHERE name = 'System Utilities');

INSERT INTO workstation_command_groups (id, tenant_id, name, description, patterns, is_builtin, created_by)
SELECT gen_random_uuid(), NULL, 'Network Tools', 'Network diagnostics',
       '["ping","traceroute","curl","wget","nslookup","dig","ss","ip"]'::jsonb, TRUE, 'system'
WHERE NOT EXISTS (SELECT 1 FROM workstation_command_groups WHERE name = 'Network Tools');

INSERT INTO workstation_command_groups (id, tenant_id, name, description, patterns, is_builtin, created_by)
SELECT gen_random_uuid(), NULL, 'Default Commands', 'Safe commands automatically allowed on all workstations',
       '["echo","pwd","ls","cat","git","env","whoami","hostname","date","uname","claude","cd","pushd","popd","dirs","printf","read","export","unset","declare","typeset","local","test","true","false","set","shift","exit","wait","source",".","type","help","history","times","builtin","command","shopt","ulimit","umask","mapfile","readarray","caller","enable","compgen","complete","compopt"]'::jsonb, TRUE, 'system'
WHERE NOT EXISTS (SELECT 1 FROM workstation_command_groups WHERE name = 'Default Commands');

-- From 000078_workstation_activity_agent_idx.up.sql
CREATE INDEX IF NOT EXISTS idx_ws_activity_agent_time ON workstation_activity(agent_id, created_at DESC);

-- From 000079_multi_auth_module.up.sql
-- Multi-Auth Module: User, Group & RBAC tables
-- SRS: docs/srs/multi-auth-module-srs.md

-- ============================================================
-- Table: users
-- Identity table for authenticated users. Replaces reliance on
-- tenant_users.user_id VARCHAR(255) for identity. User records
-- are created via auto-provisioning (OIDC/local auth) or
-- pre-provisioning (tenant admin).
-- ============================================================

CREATE TABLE users (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email            VARCHAR(255) NOT NULL,
    display_name     VARCHAR(255) NOT NULL,
    avatar_url       TEXT,
    tenant_id        UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    auth_provider    VARCHAR(20) NOT NULL CHECK (auth_provider IN ('local', 'entra_id', 'google')),
    password_hash    TEXT,
    is_tenant_admin  BOOLEAN NOT NULL DEFAULT false,
    status           VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'deactivated')),
    last_login_at    TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, email)
);

CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_tenant ON users(tenant_id);
CREATE INDEX idx_users_tenant_email ON users(tenant_id, email);
CREATE INDEX idx_users_status ON users(tenant_id, status);

-- ============================================================
-- Table: user_identities
-- Multi-provider identity linking. A user may authenticate via
-- multiple providers (e.g. Entra ID + Google). Each provider
-- identity links to one User record.
-- ============================================================

CREATE TABLE user_identities (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider         VARCHAR(20) NOT NULL CHECK (provider IN ('local', 'entra_id', 'google')),
    provider_subject VARCHAR(255) NOT NULL,
    provider_tenant  VARCHAR(255),
    email            VARCHAR(255) NOT NULL,
    linked_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at     TIMESTAMPTZ,
    UNIQUE(user_id, provider),
    UNIQUE(provider, provider_subject)
);

CREATE INDEX idx_user_identities_user ON user_identities(user_id);
CREATE INDEX idx_user_identities_email ON user_identities(email);
CREATE INDEX idx_user_identities_lookup ON user_identities(provider, provider_subject);

-- ============================================================
-- Table: groups
-- Organizational groups (departments, teams, units) with
-- optional parent-child hierarchy (max 3 levels).
-- ============================================================

CREATE TABLE groups (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(255) NOT NULL,
    slug            VARCHAR(100) NOT NULL,
    description     TEXT,
    parent_group_id UUID REFERENCES groups(id) ON DELETE SET NULL,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    visibility      VARCHAR(20) NOT NULL DEFAULT 'closed' CHECK (visibility IN ('open', 'closed')),
    max_members     INTEGER NOT NULL DEFAULT 0,
    created_by      UUID REFERENCES users(id),
    status          VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'deleted')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(slug, tenant_id)
);

CREATE INDEX idx_groups_slug ON groups(slug);
CREATE INDEX idx_groups_tenant ON groups(tenant_id);
CREATE INDEX idx_groups_parent ON groups(parent_group_id);
CREATE INDEX idx_groups_tenant_status ON groups(tenant_id, status) WHERE status = 'active';

-- Enforce max hierarchy depth (3 levels) via trigger.
-- Root: parent_group_id IS NULL (level 1)
-- Child: parent's parent_group_id IS NULL (level 2)
-- Grandchild: parent's parent has parent_group_id IS NULL (level 3)
-- Anything deeper is rejected.
CREATE OR REPLACE FUNCTION check_group_hierarchy_depth()
RETURNS TRIGGER AS $$
DECLARE
    parent_depth INTEGER;
BEGIN
    IF NEW.parent_group_id IS NULL THEN
        RETURN NEW;
    END IF;

    WITH RECURSIVE ancestors AS (
        SELECT id, parent_group_id, 1 AS depth FROM groups WHERE id = NEW.parent_group_id
        UNION ALL
        SELECT g.id, g.parent_group_id, a.depth + 1
        FROM groups g JOIN ancestors a ON g.id = a.parent_group_id
    )
    SELECT COALESCE(MAX(depth), 0) INTO parent_depth FROM ancestors;

    IF parent_depth >= 3 THEN
        RAISE EXCEPTION 'group hierarchy exceeds maximum depth of 3 levels';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_group_hierarchy_depth
    BEFORE INSERT OR UPDATE OF parent_group_id ON groups
    FOR EACH ROW EXECUTE FUNCTION check_group_hierarchy_depth();

-- ============================================================
-- Table: group_members
-- Membership with role (admin/member). Group admins can manage
-- members and approve requests within their group.
-- ============================================================

CREATE TABLE group_members (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id   UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       VARCHAR(20) NOT NULL DEFAULT 'member' CHECK (role IN ('admin', 'member')),
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    joined_via VARCHAR(20) NOT NULL DEFAULT 'admin_add' CHECK (joined_via IN ('admin_add', 'self_join', 'request_approved')),
    UNIQUE(group_id, user_id)
);

CREATE INDEX idx_group_members_group ON group_members(group_id);
CREATE INDEX idx_group_members_user ON group_members(user_id);
CREATE INDEX idx_group_members_group_role ON group_members(group_id, role);

-- ============================================================
-- Table: join_requests
-- Self-service join requests for closed groups.
-- ============================================================

CREATE TABLE join_requests (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id     UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status       VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    reviewed_by  UUID REFERENCES users(id),
    reviewed_at  TIMESTAMPTZ,
    message      TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(group_id, user_id, status) -- one active request per user per group
);

CREATE INDEX idx_join_requests_group_status ON join_requests(group_id, status);
CREATE INDEX idx_join_requests_user ON join_requests(user_id);

-- ============================================================
-- Table: audit_log
-- Append-only audit trail for compliance. Retained 7 years.
-- ============================================================

CREATE TABLE audit_log (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    actor_id       UUID REFERENCES users(id),
    action         VARCHAR(100) NOT NULL,
    resource_type  VARCHAR(20) NOT NULL CHECK (resource_type IN ('user', 'group', 'membership', 'permission', 'system')),
    resource_id    UUID NOT NULL,
    group_id       UUID REFERENCES groups(id) ON DELETE SET NULL,
    detail         JSONB,
    ip_address     VARCHAR(45),
    user_agent     TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_log_tenant ON audit_log(tenant_id);
CREATE INDEX idx_audit_log_actor ON audit_log(tenant_id, actor_id);
CREATE INDEX idx_audit_log_resource ON audit_log(tenant_id, resource_type, resource_id);
CREATE INDEX idx_audit_log_group ON audit_log(tenant_id, group_id);
CREATE INDEX idx_audit_log_created ON audit_log(tenant_id, created_at DESC);

-- ============================================================
-- Table: refresh_tokens
-- Persistent refresh tokens for JWT session management.
-- One active refresh token per user per device/session.
-- ============================================================

CREATE TABLE refresh_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  VARCHAR(64) NOT NULL UNIQUE,
    device_info TEXT,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_refresh_tokens_user ON refresh_tokens(user_id);
CREATE INDEX idx_refresh_tokens_hash ON refresh_tokens(token_hash);
CREATE INDEX idx_refresh_tokens_expires ON refresh_tokens(expires_at);

-- From 000080_drop_is_tenant_admin.up.sql
-- Drop is_tenant_admin column from users table.
-- Tenant roles now live in tenant_users.role only.
