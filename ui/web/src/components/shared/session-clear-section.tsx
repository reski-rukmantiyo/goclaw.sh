import { useTranslation } from "react-i18next";
import { Switch } from "@/components/ui/switch";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ConfigGroupHeader } from "@/components/shared/config-group-header";
import { Combobox } from "@/components/ui/combobox";
import { getAllIanaTimezones } from "@/lib/constants";

export interface SessionClearConfig {
  enabled?: boolean;
  schedule?: {
    kind?: "every" | "cron";
    everyMs?: number;
    expr?: string;
    tz?: string;
  };
  action?: "reset" | "delete";
  scope?: "all" | "dm" | "group";
}

interface Props {
  value: SessionClearConfig | undefined;
  onChange: (value: SessionClearConfig | undefined) => void;
  /** Hide scope field (for per-group overrides — scope is always "all") */
  hideScope?: boolean;
}

const MS_PER_HOUR = 3600000;

function msToHours(ms: number): number {
  return Math.round(ms / MS_PER_HOUR);
}

function hoursToMs(h: number): number {
  return h * MS_PER_HOUR;
}

export function SessionClearSection({ value, onChange, hideScope }: Props) {
  const { t } = useTranslation("channels");

  const enabled = value?.enabled ?? false;
  const schedule = value?.schedule;
  const action = value?.action ?? "reset";
  const scope = value?.scope ?? "all";
  const kind = schedule?.kind ?? "every";
  const everyHours = schedule?.everyMs ? msToHours(schedule.everyMs) : 24;
  const expr = schedule?.expr ?? "";
  const tz = schedule?.tz ?? "";

  const update = (patch: Partial<SessionClearConfig>) => {
    const merged = { ...value, ...patch };
    onChange(merged.enabled ? merged : undefined);
  };

  const updateSchedule = (patch: Partial<NonNullable<SessionClearConfig["schedule"]>>) => {
    const current = value ?? {};
    const currentSchedule = current.schedule ?? {};
    const newSchedule = { ...currentSchedule, ...patch };

    if (kind === "every" || patch.kind === "every") {
      delete newSchedule.expr;
    } else {
      delete newSchedule.everyMs;
    }

    update({ ...current, schedule: newSchedule });
  };

  return (
    <fieldset className="space-y-3">
      <ConfigGroupHeader title={t("sessionClear.title")} description={t("sessionClear.enabledHint")} />

      {/* Enable toggle */}
      <div className="flex items-center justify-between gap-2">
        <div className="space-y-0.5">
          <Label className="text-xs font-medium">{t("sessionClear.enabledLabel")}</Label>
          <p className="text-xs text-muted-foreground">{t("sessionClear.enabledHint")}</p>
        </div>
        <Switch
          checked={enabled}
          onCheckedChange={(v) => {
            if (v) {
              onChange({ enabled: true, action: "reset", scope: "all", schedule: { kind: "every", everyMs: hoursToMs(24) } });
            } else {
              onChange(undefined);
            }
          }}
        />
      </div>

      {enabled && (
        <>
          {/* Action */}
          <div className="space-y-1.5">
            <Label className="text-xs font-medium">{t("sessionClear.actionLabel")}</Label>
            <Select value={action} onValueChange={(v) => update({ action: v as "reset" | "delete" })}>
              <SelectTrigger className="h-9">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="reset">{t("sessionClear.actionReset")}</SelectItem>
                <SelectItem value="delete">{t("sessionClear.actionDelete")}</SelectItem>
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              {action === "reset" ? t("sessionClear.actionResetHint") : t("sessionClear.actionDeleteHint")}
            </p>
          </div>

          {/* Scope (channel-level only) */}
          {!hideScope && (
            <div className="space-y-1.5">
              <Label className="text-xs font-medium">{t("sessionClear.scopeLabel")}</Label>
              <Select value={scope} onValueChange={(v) => update({ scope: v as "all" | "dm" | "group" })}>
                <SelectTrigger className="h-9">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">{t("sessionClear.scopeAll")}</SelectItem>
                  <SelectItem value="dm">{t("sessionClear.scopeDm")}</SelectItem>
                  <SelectItem value="group">{t("sessionClear.scopeGroup")}</SelectItem>
                </SelectContent>
              </Select>
            </div>
          )}

          {/* Schedule kind */}
          <div className="space-y-1.5">
            <Label className="text-xs font-medium">{t("sessionClear.scheduleKindLabel")}</Label>
            <Select value={kind} onValueChange={(v) => updateSchedule({ kind: v as "every" | "cron" })}>
              <SelectTrigger className="h-9">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="every">{t("sessionClear.kindEvery")}</SelectItem>
                <SelectItem value="cron">{t("sessionClear.kindCron")}</SelectItem>
              </SelectContent>
            </Select>
          </div>

          {/* Interval (every) */}
          {kind === "every" && (
            <div className="space-y-1.5">
              <Label className="text-xs font-medium">{t("sessionClear.intervalLabel")}</Label>
              <div className="flex items-center gap-2">
                <Input
                  type="number"
                  min={1}
                  value={everyHours}
                  onChange={(e) => updateSchedule({ everyMs: hoursToMs(parseInt(e.target.value) || 1) })}
                  className="h-9 w-24"
                />
                <span className="text-xs text-muted-foreground">{t("sessionClear.hours")}</span>
              </div>
            </div>
          )}

          {/* Cron expression */}
          {kind === "cron" && (
            <div className="space-y-1.5">
              <Label className="text-xs font-medium">{t("sessionClear.cronExprLabel")}</Label>
              <Input
                value={expr}
                onChange={(e) => updateSchedule({ expr: e.target.value })}
                placeholder="0 0 * * *"
                className="h-9 text-sm font-mono"
              />
              <p className="text-xs text-muted-foreground">{t("sessionClear.cronExprHint")}</p>
            </div>
          )}

          {/* Timezone (for cron) */}
          {kind === "cron" && (
            <div className="space-y-1.5">
              <Label className="text-xs font-medium">{t("sessionClear.timezoneLabel")}</Label>
              <Combobox
                value={tz || "UTC"}
                onChange={(v) => updateSchedule({ tz: v === "UTC" ? undefined : v })}
                options={getAllIanaTimezones()}
                placeholder="UTC"
              />
              <p className="text-xs text-muted-foreground">{t("sessionClear.timezoneHint")}</p>
            </div>
          )}
        </>
      )}
    </fieldset>
  );
}
