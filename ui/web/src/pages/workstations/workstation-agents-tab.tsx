import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Link2, Unlink } from "lucide-react";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useWorkstations } from "./hooks/use-workstations";

interface WorkstationAgentsTabProps {
  workstationId: string;
}

export function WorkstationAgentsTab({ workstationId }: WorkstationAgentsTabProps) {
  const { t } = useTranslation("workstations");
  const { agents } = useAgents();
  const { listLinkedAgents, linkAgent, unlinkAgent } = useWorkstations();

  const [links, setLinks] = useState<{ agentId: string; isDefault: boolean }[]>([]);
  const [loading, setLoading] = useState(false);
  const [selectedAgentId, setSelectedAgentId] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await listLinkedAgents(workstationId);
      setLinks(res);
    } catch {
      setLinks([]);
    } finally {
      setLoading(false);
    }
  }, [workstationId, listLinkedAgents]);

  useEffect(() => {
    load();
  }, [load]);

  const handleLink = async () => {
    if (!selectedAgentId) return;
    await linkAgent(workstationId, selectedAgentId);
    setSelectedAgentId("");
    await load();
  };

  const handleUnlink = async (agentId: string) => {
    await unlinkAgent(workstationId, agentId);
    await load();
  };

  const linkedAgentIds = new Set(links.map((l) => l.agentId));
  const availableAgents = agents.filter((a) => !linkedAgentIds.has(a.id));

  return (
    <div className="space-y-4">
      <div className="flex items-end gap-2">
        <div className="flex-1">
          <label className="text-sm font-medium mb-1.5 block">
            {t("agents.linkLabel", "Link Agent")}
          </label>
          <Select value={selectedAgentId} onValueChange={setSelectedAgentId}>
            <SelectTrigger className="text-base md:text-sm">
              <SelectValue placeholder={t("agents.selectAgent", "Select agent...")} />
            </SelectTrigger>
            <SelectContent>
              {availableAgents.map((agent) => (
                <SelectItem key={agent.id} value={agent.id}>
                  {agent.display_name || agent.agent_key || agent.id}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <Button
          size="sm"
          onClick={handleLink}
          disabled={!selectedAgentId || loading}
          className="gap-1"
        >
          <Link2 className="h-3.5 w-3.5" />
          {t("agents.link", "Link")}
        </Button>
      </div>

      {links.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {t("agents.empty", "No agents linked to this workstation.")}
        </p>
      ) : (
        <div className="space-y-2">
          {links.map((link) => {
            const agent = agents.find((a) => a.id === link.agentId);
            return (
              <div
                key={link.agentId}
                className="flex items-center justify-between rounded-md border px-3 py-2"
              >
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium">
                    {agent?.display_name || agent?.agent_key || link.agentId}
                  </span>
                  {link.isDefault && (
                    <Badge variant="outline" className="text-xs">
                      {t("agents.default", "Default")}
                    </Badge>
                  )}
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => handleUnlink(link.agentId)}
                  disabled={loading}
                  className="gap-1 text-destructive"
                >
                  <Unlink className="h-3.5 w-3.5" />
                  {t("agents.unlink", "Unlink")}
                </Button>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
