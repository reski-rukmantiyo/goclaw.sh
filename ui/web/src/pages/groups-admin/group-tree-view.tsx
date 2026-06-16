import { useState } from "react";
import { ChevronRight, ChevronDown, Pencil, Trash2, Users, UserPlus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import type { GroupTreeNode } from "@/types/user-mgmt";
import { useTranslation } from "react-i18next";

function visibilityBadge(visibility: string) {
  if (visibility === "open") return "default" as const;
  return "secondary" as const;
}

interface TreeNodeProps {
  node: GroupTreeNode;
  depth: number;
  groupMap: Map<string, { name: string }>;
  onEdit: (group: GroupTreeNode) => void;
  onDelete: (group: GroupTreeNode) => void;
  onManageMembers: (groupId: string) => void;
  onViewRequests: (groupId: string) => void;
}

function TreeNode({ node, depth, groupMap, onEdit, onDelete, onManageMembers, onViewRequests }: TreeNodeProps) {
  const { t } = useTranslation("groups-admin");
  const [expanded, setExpanded] = useState(true);
  const hasChildren = node.children && node.children.length > 0;

  return (
    <>
      <div
        className="flex items-center gap-2 border-b last:border-0 hover:bg-muted/30 py-2 px-3"
        style={{ paddingLeft: `${depth * 24 + 12}px` }}
      >
        <button
          onClick={() => hasChildren && setExpanded(!expanded)}
          className={`shrink-0 ${hasChildren ? "cursor-pointer" : "cursor-default opacity-0"}`}
        >
          {hasChildren ? (
            expanded ? (
              <ChevronDown className="h-4 w-4 text-muted-foreground" />
            ) : (
              <ChevronRight className="h-4 w-4 text-muted-foreground" />
            )
          ) : (
            <ChevronRight className="h-4 w-4" />
          )}
        </button>

        <span className="font-medium truncate">{node.name}</span>

        <code className="hidden sm:inline rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
          {node.slug}
        </code>

        <Badge variant={visibilityBadge(node.visibility)} className="text-xs">
          {t(`visibility.${node.visibility}`)}
        </Badge>

        <div className="flex-1" />

        <div className="flex items-center gap-1 shrink-0">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onManageMembers(node.id)}
            title={t("actions.manageMembers")}
          >
            <Users className="h-3.5 w-3.5" />
          </Button>
          {node.visibility === "closed" && (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => onViewRequests(node.id)}
              title={t("actions.viewRequests")}
            >
              <UserPlus className="h-3.5 w-3.5" />
            </Button>
          )}
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onEdit(node)}
            title={t("actions.edit")}
          >
            <Pencil className="h-3.5 w-3.5" />
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onDelete(node)}
            className="text-destructive hover:text-destructive"
            title={t("actions.delete")}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>

      {expanded && hasChildren &&
        node.children.map((child) => (
          <TreeNode
            key={child.id}
            node={child}
            depth={depth + 1}
            groupMap={groupMap}
            onEdit={onEdit}
            onDelete={onDelete}
            onManageMembers={onManageMembers}
            onViewRequests={onViewRequests}
          />
        ))}
    </>
  );
}

interface GroupTreeViewProps {
  tree: GroupTreeNode[];
  groupMap: Map<string, { name: string }>;
  onEdit: (group: GroupTreeNode) => void;
  onDelete: (group: GroupTreeNode) => void;
  onManageMembers: (groupId: string) => void;
  onViewRequests: (groupId: string) => void;
}

export function GroupTreeView({ tree, groupMap, onEdit, onDelete, onManageMembers, onViewRequests }: GroupTreeViewProps) {
  if (tree.length === 0) return null;

  return (
    <div className="rounded-md border">
      {tree.map((node) => (
        <TreeNode
          key={node.id}
          node={node}
          depth={0}
          groupMap={groupMap}
          onEdit={onEdit}
          onDelete={onDelete}
          onManageMembers={onManageMembers}
          onViewRequests={onViewRequests}
        />
      ))}
    </div>
  );
}
