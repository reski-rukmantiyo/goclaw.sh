import { Suspense } from "react";
import { Routes, Route, Navigate, useLocation } from "react-router";
import { LOCAL_STORAGE_KEYS } from "@/lib/constants";
import { route } from "@/lib/routes";
import { AppLayout } from "@/components/layout/app-layout";
import { RequireAuth } from "@/components/shared/require-auth";
import { RequireAdmin, RequireCrossTenant, RequireMember } from "@/components/shared/require-role";
import { RequireSetup } from "@/components/shared/require-setup";
import { ErrorBoundary } from "@/components/shared/error-boundary";
import { ROUTES } from "@/lib/constants";
import { lazyWithRetry } from "@/lib/lazy-with-retry";

// Lazy-loaded pages
const LoginPage = lazyWithRetry(() =>
  import("@/pages/login/login-page").then((m) => ({ default: m.LoginPage })),
);
const OverviewPage = lazyWithRetry(() =>
  import("@/pages/overview/overview-page").then((m) => ({ default: m.OverviewPage })),
);
const ChatPage = lazyWithRetry(() =>
  import("@/pages/chat/chat-page").then((m) => ({ default: m.ChatPage })),
);
const AgentsPage = lazyWithRetry(() =>
  import("@/pages/agents/agents-page").then((m) => ({ default: m.AgentsPage })),
);
const AgentCodexPoolPage = lazyWithRetry(() =>
  import("@/pages/agents/agent-detail/agent-codex-pool-page").then((m) => ({ default: m.AgentCodexPoolPage })),
);
const ImportExportPage = lazyWithRetry(() =>
  import("@/pages/import-export/import-export-page").then((m) => ({ default: m.ImportExportPage })),
);
const SessionsPage = lazyWithRetry(() =>
  import("@/pages/sessions/sessions-page").then((m) => ({ default: m.SessionsPage })),
);
const SkillsPage = lazyWithRetry(() =>
  import("@/pages/skills/skills-page").then((m) => ({ default: m.SkillsPage })),
);
const CronPage = lazyWithRetry(() =>
  import("@/pages/cron/cron-page").then((m) => ({ default: m.CronPage })),
);
const ConfigPage = lazyWithRetry(() =>
  import("@/pages/config/config-page").then((m) => ({ default: m.ConfigPage })),
);
const TracesPage = lazyWithRetry(() =>
  import("@/pages/traces/traces-page").then((m) => ({ default: m.TracesPage })),
);
const ChannelsPage = lazyWithRetry(() =>
  import("@/pages/channels/channels-page").then((m) => ({ default: m.ChannelsPage })),
);
const ApprovalsPage = lazyWithRetry(() =>
  import("@/pages/approvals/approvals-page").then((m) => ({ default: m.ApprovalsPage })),
);
const NodesPage = lazyWithRetry(() =>
  import("@/pages/nodes/nodes-page").then((m) => ({ default: m.NodesPage })),
);
const LogsPage = lazyWithRetry(() =>
  import("@/pages/logs/logs-page").then((m) => ({ default: m.LogsPage })),
);
const ProvidersPage = lazyWithRetry(() =>
  import("@/pages/providers/providers-page").then((m) => ({ default: m.ProvidersPage })),
);
const MCPPage = lazyWithRetry(() =>
  import("@/pages/mcp/mcp-page").then((m) => ({ default: m.MCPPage })),
);
const TeamsPage = lazyWithRetry(() =>
  import("@/pages/teams/teams-page").then((m) => ({ default: m.TeamsPage })),
);
const BuiltinToolsPage = lazyWithRetry(() =>
  import("@/pages/builtin-tools/builtin-tools-page").then((m) => ({ default: m.BuiltinToolsPage })),
);
const TtsPage = lazyWithRetry(() =>
  import("@/pages/tts/tts-page").then((m) => ({ default: m.TtsPage })),
);
const EventsPage = lazyWithRetry(() =>
  import("@/pages/events/events-page").then((m) => ({ default: m.EventsPage })),
);
const StoragePage = lazyWithRetry(() =>
  import("@/pages/storage/storage-page").then((m) => ({ default: m.StoragePage })),
);
const SetupPage = lazyWithRetry(() =>
  import("@/pages/setup/setup-page").then((m) => ({ default: m.SetupPage })),
);
const PendingMessagesPage = lazyWithRetry(() =>
  import("@/pages/pending-messages/pending-messages-page").then((m) => ({ default: m.PendingMessagesPage })),
);
const RawMessagesPage = lazyWithRetry(() =>
  import("@/pages/raw-messages/raw-messages-page").then((m) => ({ default: m.RawMessagesPage })),
);
const EmbeddingsPage = lazyWithRetry(() =>
  import("@/pages/embeddings/embeddings-page").then((m) => ({ default: m.EmbeddingsPage })),
);
const MemoryPage = lazyWithRetry(() =>
  import("@/pages/memory/memory-page").then((m) => ({ default: m.MemoryPage })),
);
const VaultPage = lazyWithRetry(() =>
  import("@/pages/vault/vault-page").then((m) => ({ default: m.VaultPage })),
);
const KnowledgeGraphPage = lazyWithRetry(() =>
  import("@/pages/knowledge-graph/knowledge-graph-page").then((m) => ({ default: m.KnowledgeGraphPage })),
);
const ContactsPage = lazyWithRetry(() =>
  import("@/pages/contacts/contacts-page").then((m) => ({ default: m.ContactsPage })),
);
const ActivityPage = lazyWithRetry(() =>
  import("@/pages/activity/activity-page").then((m) => ({ default: m.ActivityPage })),
);
const ApiKeysPage = lazyWithRetry(() =>
  import("@/pages/api-keys/api-keys-page").then((m) => ({ default: m.ApiKeysPage })),
);
const PackagesPage = lazyWithRetry(() =>
  import("@/pages/packages/packages-page").then((m) => ({ default: m.PackagesPage })),
);
const TenantsAdminPage = lazyWithRetry(() =>
  import("@/pages/tenants-admin/tenants-admin-page").then((m) => ({ default: m.TenantsAdminPage })),
);
const TenantDetailPage = lazyWithRetry(() =>
  import("@/pages/tenants-admin/tenant-detail-page").then((m) => ({ default: m.TenantDetailPage })),
);
const TenantAuthPage = lazyWithRetry(() =>
  import("@/pages/tenant-auth/tenant-auth-page").then((m) => ({ default: m.TenantAuthPage })),
);
const BackupRestorePage = lazyWithRetry(() =>
  import("@/pages/backup-restore/backup-restore-page").then((m) => ({ default: m.BackupRestorePage })),
);
const HooksPage = lazyWithRetry(() =>
  import("@/pages/hooks").then((m) => ({ default: m.HooksPage })),
);
const WorkstationsPage = lazyWithRetry(() =>
  import("@/pages/workstations/workstations-page").then((m) => ({ default: m.WorkstationsPage })),
);
const TenantSelectorPage = lazyWithRetry(() =>
  import("@/pages/login/tenant-selector").then((m) => ({ default: m.TenantSelectorPage })),
);
const UsersAdminPage = lazyWithRetry(() =>
  import("@/pages/users-admin/users-admin-page"),
);
const GroupsAdminPage = lazyWithRetry(() =>
  import("@/pages/groups-admin/groups-admin-page"),
);
const AuditLogPage = lazyWithRetry(() =>
  import("@/pages/audit-log/audit-log-page"),
);
const RoleManagementPage = lazyWithRetry(() =>
  import("@/pages/role-management/role-management-page"),
);
const ProfilePage = lazyWithRetry(() =>
  import("@/pages/profile/profile-page").then((m) => ({ default: m.ProfilePage })),
);

function LegacyRedirect() {
  const location = useLocation();
  const slug = localStorage.getItem(LOCAL_STORAGE_KEYS.TENANT_ID) || "master";
  return <Navigate to={route(slug, location.pathname)} replace />;
}

function PageLoader() {
  return (
    <div className="flex h-full items-center justify-center">
      <img src="/goclaw-icon.svg" alt="" className="h-8 w-8 animate-pulse opacity-50" />
    </div>
  );
}

export function AppRoutes() {
  return (
    <ErrorBoundary>
    <Suspense fallback={<PageLoader />}>
      <Routes>
        <Route path={ROUTES.LOGIN} element={<LoginPage />} />

        {/* Tenant selector — accessible when authenticated but tenant not yet selected */}
        <Route path={ROUTES.SELECT_TENANT} element={<TenantSelectorPage />} />

        {/* Setup wizard — standalone layout, requires auth but no sidebar */}
        <Route
          path={ROUTES.SETUP}
          element={
            <RequireAuth>
              <SetupPage />
            </RequireAuth>
          }
        />

        {/* Tenant-scoped app routes */}
        <Route path="/t/:slug">
          <Route
            element={
              <RequireAuth>
                <RequireSetup>
                  <AppLayout />
                </RequireSetup>
              </RequireAuth>
            }
          >
            <Route index element={<Navigate to="overview" replace />} />
            <Route path="profile" element={<ProfilePage />} />
            <Route path="overview" element={<OverviewPage />} />
            <Route path="chat/:sessionKey?" element={<ChatPage />} />
            <Route path="agents" element={<AgentsPage key="list" />} />
            <Route path="import-export" element={<RequireAdmin><ImportExportPage /></RequireAdmin>} />
            <Route path="backup-restore" element={<RequireAdmin><BackupRestorePage /></RequireAdmin>} />
            <Route path="agents/:id/codex-pool" element={<RequireAdmin><AgentCodexPoolPage /></RequireAdmin>} />
            <Route path="agents/:id" element={<AgentsPage key="detail" />} />
            <Route path="teams" element={<TeamsPage key="list" />} />
            <Route path="teams/:id" element={<TeamsPage key="detail" />} />
            <Route path="sessions" element={<SessionsPage key="list" />} />
            <Route path="sessions/:key" element={<SessionsPage key="detail" />} />
            <Route path="skills" element={<SkillsPage key="list" />} />
            <Route path="skills/:id" element={<SkillsPage key="detail" />} />
            <Route path="cron" element={<CronPage key="list" />} />
            <Route path="cron/:id" element={<CronPage key="detail" />} />
            <Route path="hooks" element={<HooksPage key="list" />} />
            <Route path="hooks/:id" element={<HooksPage key="detail" />} />
            {/* Admin-only pages */}
            <Route path="config" element={<RequireCrossTenant><ConfigPage /></RequireCrossTenant>} />
            <Route path="providers" element={<RequireAdmin><ProvidersPage key="list" /></RequireAdmin>} />
            <Route path="providers/:id" element={<RequireAdmin><ProvidersPage key="detail" /></RequireAdmin>} />
            <Route path="cli-credentials" element={<Navigate to="../packages?tab=cli-credentials" replace />} />
            <Route path="api-keys" element={<RequireMember><ApiKeysPage /></RequireMember>} />
            <Route path="channels" element={<ChannelsPage key="list" />} />
            <Route path="channels/:id" element={<ChannelsPage key="detail" />} />
            <Route path="nodes" element={<NodesPage />} />
            <Route path="workstations" element={<WorkstationsPage />} />
            <Route path="logs" element={<RequireMember><LogsPage /></RequireMember>} />
            <Route path="builtin-tools" element={<BuiltinToolsPage />} />
            <Route path="mcp" element={<MCPPage />} />
            <Route path="tts" element={<RequireMember><TtsPage /></RequireMember>} />
            <Route path="storage" element={<StoragePage />} />
            <Route path="packages" element={<RequireMember><PackagesPage /></RequireMember>} />
            <Route path="authentication" element={<RequireAdmin><TenantAuthPage /></RequireAdmin>} />
            <Route path="admin/tenants" element={<RequireCrossTenant><TenantsAdminPage /></RequireCrossTenant>} />
            <Route path="admin/tenants/:id" element={<RequireCrossTenant><TenantDetailPage /></RequireCrossTenant>} />
            <Route path="admin/users" element={<RequireAdmin><UsersAdminPage /></RequireAdmin>} />
            <Route path="admin/users/:id" element={<RequireAdmin><UsersAdminPage /></RequireAdmin>} />
            <Route path="admin/groups" element={<RequireAdmin><GroupsAdminPage /></RequireAdmin>} />
            <Route path="admin/groups/:id" element={<RequireAdmin><GroupsAdminPage /></RequireAdmin>} />
            <Route path="admin/audit" element={<RequireAdmin><AuditLogPage /></RequireAdmin>} />
            <Route path="admin/roles" element={<RequireAdmin><RoleManagementPage /></RequireAdmin>} />

            {/* All-role pages */}
            <Route path="traces" element={<TracesPage key="list" />} />
            <Route path="traces/:id" element={<TracesPage key="detail" />} />
            <Route path="events" element={<RequireMember><EventsPage /></RequireMember>} />
            <Route path="usage" element={<Navigate to="overview" replace />} />
            <Route path="activity" element={<RequireMember><ActivityPage /></RequireMember>} />
            <Route path="contacts" element={<ContactsPage />} />
            <Route path="approvals" element={<ApprovalsPage />} />
            <Route path="pending-messages" element={<PendingMessagesPage />} />
            <Route path="raw-messages" element={<RawMessagesPage />} />
            <Route path="embeddings" element={<EmbeddingsPage />} />
            <Route path="memory" element={<MemoryPage />} />
            <Route path="vault" element={<VaultPage />} />
            <Route path="knowledge-graph" element={<KnowledgeGraphPage />} />
          </Route>
        </Route>

        {/* Legacy routes — redirect to tenant-scoped */}
        <Route path={ROUTES.OVERVIEW} element={<LegacyRedirect />} />
        <Route path={ROUTES.PROFILE} element={<LegacyRedirect />} />
        <Route path={ROUTES.CHAT_PATTERN} element={<LegacyRedirect />} />
        <Route path={ROUTES.AGENTS} element={<LegacyRedirect />} />
        <Route path={ROUTES.AGENT_DETAIL} element={<LegacyRedirect />} />
        <Route path={ROUTES.AGENT_CODEX_POOL} element={<LegacyRedirect />} />
        <Route path={ROUTES.TEAMS} element={<LegacyRedirect />} />
        <Route path={ROUTES.TEAM_DETAIL} element={<LegacyRedirect />} />
        <Route path={ROUTES.SESSIONS} element={<LegacyRedirect />} />
        <Route path={ROUTES.SESSION_DETAIL} element={<LegacyRedirect />} />
        <Route path={ROUTES.SKILLS} element={<LegacyRedirect />} />
        <Route path={ROUTES.SKILL_DETAIL} element={<LegacyRedirect />} />
        <Route path={ROUTES.CRON} element={<LegacyRedirect />} />
        <Route path={ROUTES.CRON_DETAIL} element={<LegacyRedirect />} />
        <Route path={ROUTES.CONFIG} element={<LegacyRedirect />} />
        <Route path={ROUTES.AUTHENTICATION} element={<LegacyRedirect />} />
        <Route path={ROUTES.PROVIDERS} element={<LegacyRedirect />} />
        <Route path={ROUTES.PROVIDER_DETAIL} element={<LegacyRedirect />} />
        <Route path={ROUTES.API_KEYS} element={<LegacyRedirect />} />
        <Route path={ROUTES.CHANNELS} element={<LegacyRedirect />} />
        <Route path={ROUTES.CHANNEL_DETAIL} element={<LegacyRedirect />} />
        <Route path={ROUTES.NODES} element={<LegacyRedirect />} />
        <Route path={ROUTES.WORKSTATIONS} element={<LegacyRedirect />} />
        <Route path={ROUTES.LOGS} element={<LegacyRedirect />} />
        <Route path={ROUTES.BUILTIN_TOOLS} element={<LegacyRedirect />} />
        <Route path={ROUTES.MCP} element={<LegacyRedirect />} />
        <Route path={ROUTES.TTS} element={<LegacyRedirect />} />
        <Route path={ROUTES.STORAGE} element={<LegacyRedirect />} />
        <Route path={ROUTES.PACKAGES} element={<LegacyRedirect />} />
        <Route path={ROUTES.TRACES} element={<LegacyRedirect />} />
        <Route path={ROUTES.TRACE_DETAIL} element={<LegacyRedirect />} />
        <Route path={ROUTES.EVENTS} element={<LegacyRedirect />} />
        <Route path={ROUTES.PENDING_MESSAGES} element={<LegacyRedirect />} />
        <Route path={ROUTES.RAW_MESSAGES} element={<LegacyRedirect />} />
        <Route path={ROUTES.EMBEDDINGS} element={<LegacyRedirect />} />
        <Route path={ROUTES.MEMORY} element={<LegacyRedirect />} />
        <Route path={ROUTES.VAULT} element={<LegacyRedirect />} />
        <Route path={ROUTES.KNOWLEDGE_GRAPH} element={<LegacyRedirect />} />
        <Route path={ROUTES.CONTACTS} element={<LegacyRedirect />} />
        <Route path={ROUTES.ACTIVITY} element={<LegacyRedirect />} />
        <Route path={ROUTES.APPROVALS} element={<LegacyRedirect />} />
        <Route path={ROUTES.HOOKS} element={<LegacyRedirect />} />
        <Route path={ROUTES.HOOK_DETAIL} element={<LegacyRedirect />} />
        <Route path={ROUTES.TENANTS} element={<LegacyRedirect />} />
        <Route path={ROUTES.TENANT_DETAIL} element={<LegacyRedirect />} />
        <Route path={ROUTES.USER_MGMT} element={<LegacyRedirect />} />
        <Route path={ROUTES.USER_MGMT_DETAIL} element={<LegacyRedirect />} />
        <Route path={ROUTES.GROUPS} element={<LegacyRedirect />} />
        <Route path={ROUTES.GROUP_DETAIL} element={<LegacyRedirect />} />
        <Route path={ROUTES.AUDIT_LOG} element={<LegacyRedirect />} />
        <Route path={ROUTES.BACKUP_RESTORE} element={<LegacyRedirect />} />
        <Route path={ROUTES.IMPORT_EXPORT} element={<LegacyRedirect />} />
        <Route path={ROUTES.SETUP} element={<LegacyRedirect />} />
        <Route path="*" element={<Navigate to={ROUTES.LOGIN} replace />} />
      </Routes>
    </Suspense>
    </ErrorBoundary>
  );
}
