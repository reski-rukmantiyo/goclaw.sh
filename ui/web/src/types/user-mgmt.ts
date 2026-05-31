export interface User {
  id: string;
  email: string;
  display_name: string;
  avatar_url?: string | null;
  tenant_id: string;
  auth_provider: "local" | "entra_id" | "google";
  status: "active" | "suspended" | "deactivated";
  last_login_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface UserIdentity {
  id: string;
  user_id: string;
  provider: "local" | "entra_id" | "google";
  provider_subject: string;
  provider_tenant?: string | null;
  email: string;
  linked_at: string;
  last_used_at?: string | null;
}

export interface Group {
  id: string;
  name: string;
  slug: string;
  description?: string | null;
  parent_group_id?: string | null;
  tenant_id: string;
  visibility: "open" | "closed";
  max_members: number;
  created_by: string;
  status: "active" | "deleted";
  created_at: string;
  updated_at: string;
}

export interface GroupTreeNode extends Group {
  children: GroupTreeNode[];
}

export interface GroupMember {
  id: string;
  group_id: string;
  user_id: string;
  role: "admin" | "member";
  joined_at: string;
  joined_via: "admin_add" | "self_join" | "request_approved";
  display_name?: string;
  email?: string;
}

export interface JoinRequest {
  id: string;
  group_id: string;
  user_id: string;
  status: "pending" | "approved" | "rejected";
  reviewed_by?: string | null;
  reviewed_at?: string | null;
  message?: string | null;
  created_at: string;
  display_name?: string;
  email?: string;
}

export interface AuditLogEntry {
  id: string;
  tenant_id: string;
  actor_id: string;
  action: string;
  resource_type: "user" | "group" | "membership" | "permission" | "system";
  resource_id: string;
  group_id?: string | null;
  detail?: Record<string, unknown> | null;
  ip_address?: string | null;
  user_agent?: string | null;
  created_at: string;
}

export interface PaginatedResult<T> {
  items: T[];
  total: number;
  offset: number;
  limit: number;
}

export interface AuthProvider {
  name: string;
  enabled: boolean;
}

export interface LoginResponse {
  access_token: string;
  refresh_token: string;
  expires_in: number;
  user: User;
}

export interface AuthProvidersResponse {
  providers: AuthProvider[];
}
