import {
  LayoutDashboard,
  MessageSquare,
  Bot,
  History,
  Zap,
  Clock,
  Activity,
  Radio,
  Radar,
  Terminal,
  Settings,
  ShieldCheck,
  Users,
  Link,
  Package,
  Blocks,
  Plug,
  Volume2,
  Cpu,
  ClipboardList,
  HardDrive,
  Inbox,
  Brain,
  Network,
  Contact,
  KeyRound,
  Building2,
  ArrowLeftRight,
  FileArchive,
  DatabaseBackup,
  FileText,
  Webhook,
  Layers,
  MonitorCog,
  FolderTree,

} from "lucide-react";
import { useTranslation } from "react-i18next";
import { SidebarGroup } from "./sidebar-group";
import { SidebarItem } from "./sidebar-item";
import { ConnectionStatus } from "./connection-status";
import { ROUTES, route } from "@/lib/constants";
import { cn } from "@/lib/utils";
import { usePendingPairingsCount } from "@/hooks/use-pending-pairings-count";
import { useAuthStore } from "@/stores/use-auth-store";
import { useTenants } from "@/hooks/use-tenants";

interface SidebarProps {
  collapsed: boolean;
  onNavItemClick?: () => void;
}

export function Sidebar({ collapsed, onNavItemClick }: SidebarProps) {
  const { t } = useTranslation("sidebar");
  const { pendingCount } = usePendingPairingsCount();
  const role = useAuthStore((s) => s.role);
  const { isOwner, currentTenantSlug } = useTenants();
  const isAdmin = role === "admin" || role === "owner";

  return (
    <aside
      className={cn(
        "flex h-full flex-col border-r bg-sidebar text-sidebar-foreground transition-all duration-200",
        collapsed ? "w-16" : "w-64",
      )}
      onClick={(e) => {
        // Close mobile drawer when clicking a nav link
        if (onNavItemClick && (e.target as HTMLElement).closest("a")) {
          onNavItemClick();
        }
      }}
    >
      {/* Logo / title */}
      <div className="flex h-14 items-center border-b px-4">
        {!collapsed && (
          <div className="flex items-center gap-2.5">
            <img src="/goclaw-icon.svg" alt="GoClaw" className="h-8 w-8" />
            <span className="text-lg font-bold tracking-tight text-sidebar-primary">
              GoClaw
            </span>
          </div>
        )}
        {collapsed && (
          <img src="/goclaw-icon.svg" alt="GoClaw" className="mx-auto h-7 w-7" />
        )}
      </div>

      {/* Nav items */}
      <nav className="flex-1 space-y-4 overflow-y-auto px-2 py-4">
        <SidebarGroup label={t("groups.core")} collapsed={collapsed}>
          <SidebarItem to={route(currentTenantSlug, ROUTES.OVERVIEW)} icon={LayoutDashboard} label={t("nav.overview")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.CHAT)} icon={MessageSquare} label={t("nav.chat")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.AGENTS)} icon={Bot} label={t("nav.agents")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.TEAMS)} icon={Users} label={t("nav.agentTeams")} collapsed={collapsed} />
        </SidebarGroup>

        <SidebarGroup label={t("groups.conversations")} collapsed={collapsed}>
          <SidebarItem to={route(currentTenantSlug, ROUTES.SESSIONS)} icon={History} label={t("nav.sessions")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.PENDING_MESSAGES)} icon={Inbox} label={t("nav.pendingMessages")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.RAW_MESSAGES)} icon={FileText} label={t("nav.rawMessages")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.CONTACTS)} icon={Contact} label={t("nav.contacts")} collapsed={collapsed} />
        </SidebarGroup>

        <SidebarGroup label={t("groups.connectivity")} collapsed={collapsed}>
          <SidebarItem to={route(currentTenantSlug, ROUTES.CHANNELS)} icon={Radio} label={t("nav.channels")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.NODES)} icon={Link} label={t("nav.nodes")} collapsed={collapsed} badge={pendingCount} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.WORKSTATIONS)} icon={MonitorCog} label={t("nav.workstations")} collapsed={collapsed} />
        </SidebarGroup>

        <SidebarGroup label={t("groups.capabilities")} collapsed={collapsed}>
          <SidebarItem to={route(currentTenantSlug, ROUTES.SKILLS)} icon={Zap} label={t("nav.skills")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.BUILTIN_TOOLS)} icon={Package} label={t("nav.builtinTools")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.MCP)} icon={Plug} label={t("nav.mcpServers")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.TTS)} icon={Volume2} label={t("nav.tts")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.CRON)} icon={Clock} label={t("nav.cron")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.HOOKS)} icon={Webhook} label={t("nav.hooks")} collapsed={collapsed} />
        </SidebarGroup>

        <SidebarGroup label={t("groups.data")} collapsed={collapsed}>
          <SidebarItem to={route(currentTenantSlug, ROUTES.MEMORY)} icon={Brain} label={t("nav.memory")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.VAULT)} icon={FileArchive} label={t("nav.vault")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.KNOWLEDGE_GRAPH)} icon={Network} label={t("nav.knowledgeGraph")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.EMBEDDINGS)} icon={Layers} label={t("nav.embeddings")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.STORAGE)} icon={HardDrive} label={t("nav.storage")} collapsed={collapsed} />
        </SidebarGroup>

        <SidebarGroup label={t("groups.monitoring")} collapsed={collapsed}>
          <SidebarItem to={route(currentTenantSlug, ROUTES.TRACES)} icon={Activity} label={t("nav.traces")} collapsed={collapsed} />
          {isAdmin && (
            <>
              <SidebarItem to={route(currentTenantSlug, ROUTES.EVENTS)} icon={Radar} label={t("nav.realtimeEvents")} collapsed={collapsed} />
              <SidebarItem to={route(currentTenantSlug, ROUTES.ACTIVITY)} icon={ClipboardList} label={t("nav.activity")} collapsed={collapsed} />
              <SidebarItem to={route(currentTenantSlug, ROUTES.LOGS)} icon={Terminal} label={t("nav.logs")} collapsed={collapsed} />
            </>
          )}
        </SidebarGroup>

        {isAdmin && (
        <SidebarGroup label={t("groups.system")} collapsed={collapsed}>
          <SidebarItem to={route(currentTenantSlug, ROUTES.USER_MGMT)} icon={Users} label={t("nav.userMgmt")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.GROUPS)} icon={FolderTree} label={t("nav.groups")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.ROLE_MGMT)} icon={ShieldCheck} label={t("nav.roles")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.AUDIT_LOG)} icon={FileText} label={t("nav.auditLog")} collapsed={collapsed} />
          {isOwner && (
            <SidebarItem to={route(currentTenantSlug, ROUTES.TENANTS)} icon={Building2} label={t("nav.tenants")} collapsed={collapsed} />
          )}
          <SidebarItem to={route(currentTenantSlug, ROUTES.PROVIDERS)} icon={Cpu} label={t("nav.providers")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.CLI_CREDENTIALS)} icon={KeyRound} label={t("nav.cliCredentials")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.API_KEYS)} icon={KeyRound} label={t("nav.apiKeys")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.PACKAGES)} icon={Blocks} label={t("nav.packages")} collapsed={collapsed} />
          {isOwner && (
            <SidebarItem to={route(currentTenantSlug, ROUTES.CONFIG)} icon={Settings} label={t("nav.config")} collapsed={collapsed} />
          )}
          <SidebarItem to={route(currentTenantSlug, ROUTES.AUTHENTICATION)} icon={ShieldCheck} label={t("nav.authentication")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.APPROVALS)} icon={ShieldCheck} label={t("nav.approvals")} collapsed={collapsed} />
          <SidebarItem to={route(currentTenantSlug, ROUTES.IMPORT_EXPORT)} icon={ArrowLeftRight} label={t("nav.importExport")} collapsed={collapsed} />
          {isOwner && (
            <SidebarItem to={route(currentTenantSlug, ROUTES.BACKUP_RESTORE)} icon={DatabaseBackup} label={t("nav.backupRestore")} collapsed={collapsed} />
          )}
        </SidebarGroup>
        )}
      </nav>

      {/* Footer: connection status */}
      <div className={cn("border-t py-3", collapsed ? "px-2 flex justify-center" : "px-4")}>
        <ConnectionStatus collapsed={collapsed} />
      </div>
    </aside>
  );
}
