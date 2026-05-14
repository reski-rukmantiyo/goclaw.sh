import { useState, useEffect } from "react";
import { Save } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { InfoLabel } from "@/components/shared/info-label";
import { TagInput } from "@/components/shared/tag-input";

type ToolsData = Record<string, any>;

interface Props {
  data: ToolsData | undefined;
  onSave: (value: ToolsData) => Promise<void>;
  saving: boolean;
}

export function AutoApprovedCommandsSection({ data, onSave, saving }: Props) {
  const { t } = useTranslation("config");
  const [draft, setDraft] = useState<ToolsData>(data ?? {});
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setDraft(data ?? {});
    setDirty(false);
  }, [data]);

  if (!data) return null;

  const exec = draft.execApproval ?? {};
  const allowlist: string[] = exec.allowlist ?? [];

  const updateNested = (section: string, patch: Record<string, any>) => {
    setDraft((prev) => ({
      ...prev,
      [section]: { ...(prev[section] ?? {}), ...patch },
    }));
    setDirty(true);
  };

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{t("tools.autoApprovedCommands")}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-1.5">
          <InfoLabel tip={t("tools.execAskModeTip")}>{t("tools.execAskMode")}</InfoLabel>
          <Select value={exec.ask ?? "off"} onValueChange={(v) => updateNested("execApproval", { ask: v })}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="off">Off</SelectItem>
              <SelectItem value="on-miss">On Miss</SelectItem>
              <SelectItem value="always">Always</SelectItem>
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">{t(`tools.execAskMode_${exec.ask ?? "off"}`)}</p>
        </div>

        <div className="grid gap-1.5">
          <InfoLabel tip={t("tools.autoApprovedCommandsTip")}>
            {t("tools.autoApprovedCommandsLabel")}
          </InfoLabel>
          <TagInput
            value={allowlist}
            onChange={(v) => updateNested("execApproval", { allowlist: v })}
            placeholder={t("tools.autoApprovedCommandsPlaceholder")}
          />
        </div>

        {dirty && (
          <div className="flex justify-end pt-2">
            <Button size="sm" onClick={() => onSave(draft)} disabled={saving} className="gap-1.5">
              <Save className="h-3.5 w-3.5" /> {saving ? t("saving") : t("save")}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
