import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ExternalLink } from "lucide-react";
import type { OpenRouterRoutingConfig } from "@/types/provider";

interface ProviderRoutingSectionProps {
  routing: OpenRouterRoutingConfig;
  onChange: (routing: OpenRouterRoutingConfig) => void;
}

const arrToStr = (arr?: string[]): string => (arr ?? []).join(", ");
const strToArr = (val: string): string[] =>
  val
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);

/** Hook to manage comma-separated text input synced with a string[] field. */
function useCommaListField(arr: string[] | undefined, onCommit: (val: string[]) => void) {
  const [text, setText] = useState(() => arrToStr(arr));

  // Sync from parent when the underlying array changes externally (e.g. provider reload).
  useEffect(() => {
    setText(arrToStr(arr));
  }, [arr]);

  const commit = () => {
    onCommit(strToArr(text));
  };

  return { text, setText, commit };
}

export function ProviderRoutingSection({
  routing,
  onChange,
}: ProviderRoutingSectionProps) {
  const { t } = useTranslation("providers");

  const update = (patch: Partial<OpenRouterRoutingConfig>) => {
    onChange({ ...routing, ...patch });
  };

  const order = useCommaListField(routing.order, (v) => update({ order: v }));
  const only = useCommaListField(routing.only, (v) => update({ only: v }));
  const ignore = useCommaListField(routing.ignore, (v) => update({ ignore: v }));
  const quantizations = useCommaListField(routing.quantizations, (v) => update({ quantizations: v }));

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
      <div className="space-y-1">
        <h3 className="text-sm font-medium">
          {t("routing.sectionTitle")}
        </h3>
        <p className="text-xs text-muted-foreground">
          {t("routing.sectionDescription")}{" "}
          <a
            href="https://openrouter.ai/docs/guides/routing/provider-selection"
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-0.5 text-primary underline-offset-4 hover:underline"
          >
            {t("routing.docsLink")}
            <ExternalLink className="h-3 w-3" />
          </a>
        </p>
      </div>

      {/* Sort */}
      <div className="space-y-2">
        <Label>{t("routing.sort")}</Label>
        <Select
          value={routing.sort || "_default"}
          onValueChange={(v) =>
            update({
              sort: (v === "_default" ? "" : v) as OpenRouterRoutingConfig["sort"],
            })
          }
        >
          <SelectTrigger className="w-full sm:w-56">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="_default">
              {t("routing.sortDefault")}
            </SelectItem>
            <SelectItem value="price">{t("routing.sortPrice")}</SelectItem>
            <SelectItem value="throughput">
              {t("routing.sortThroughput")}
            </SelectItem>
            <SelectItem value="latency">
              {t("routing.sortLatency")}
            </SelectItem>
          </SelectContent>
        </Select>
        <p className="text-xs text-muted-foreground">
          {t("routing.sortHint")}
        </p>
      </div>

      {/* Allow Fallbacks */}
      <div className="flex items-center justify-between gap-4">
        <div className="space-y-0.5">
          <Label className="text-sm font-medium">
            {t("routing.allowFallbacks")}
          </Label>
          <p className="text-xs text-muted-foreground">
            {t("routing.allowFallbacksHint")}
          </p>
        </div>
        <Switch
          checked={routing.allow_fallbacks ?? true}
          onCheckedChange={(v) => update({ allow_fallbacks: v })}
        />
      </div>

      {/* Require Parameters */}
      <div className="flex items-center justify-between gap-4">
        <div className="space-y-0.5">
          <Label className="text-sm font-medium">
            {t("routing.requireParameters")}
          </Label>
          <p className="text-xs text-muted-foreground">
            {t("routing.requireParametersHint")}
          </p>
        </div>
        <Switch
          checked={routing.require_parameters ?? false}
          onCheckedChange={(v) => update({ require_parameters: v })}
        />
      </div>

      {/* Data Collection */}
      <div className="space-y-2">
        <Label>{t("routing.dataCollection")}</Label>
        <Select
          value={routing.data_collection || "_default"}
          onValueChange={(v) =>
            update({
              data_collection: (v === "_default"
                ? ""
                : v) as OpenRouterRoutingConfig["data_collection"],
            })
          }
        >
          <SelectTrigger className="w-full sm:w-56">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="_default">
              {t("routing.dataCollectionDefault")}
            </SelectItem>
            <SelectItem value="allow">
              {t("routing.dataCollectionAllow")}
            </SelectItem>
            <SelectItem value="deny">
              {t("routing.dataCollectionDeny")}
            </SelectItem>
          </SelectContent>
        </Select>
        <p className="text-xs text-muted-foreground">
          {t("routing.dataCollectionHint")}
        </p>
      </div>

      {/* Provider Order */}
      <div className="space-y-2">
        <Label>{t("routing.order")}</Label>
        <Input
          value={order.text}
          onChange={(e) => order.setText(e.target.value)}
          onBlur={order.commit}
          placeholder="anthropic, openai"
          className="text-base md:text-sm"
        />
        <p className="text-xs text-muted-foreground">
          {t("routing.orderHint")}
        </p>
      </div>

      {/* Only */}
      <div className="space-y-2">
        <Label>{t("routing.only")}</Label>
        <Input
          value={only.text}
          onChange={(e) => only.setText(e.target.value)}
          onBlur={only.commit}
          placeholder="anthropic, openai"
          className="text-base md:text-sm"
        />
        <p className="text-xs text-muted-foreground">
          {t("routing.onlyHint")}
        </p>
      </div>

      {/* Ignore */}
      <div className="space-y-2">
        <Label>{t("routing.ignore")}</Label>
        <Input
          value={ignore.text}
          onChange={(e) => ignore.setText(e.target.value)}
          onBlur={ignore.commit}
          placeholder="together, deepinfra"
          className="text-base md:text-sm"
        />
        <p className="text-xs text-muted-foreground">
          {t("routing.ignoreHint")}
        </p>
      </div>

      {/* Quantizations */}
      <div className="space-y-2">
        <Label>{t("routing.quantizations")}</Label>
        <Input
          value={quantizations.text}
          onChange={(e) => quantizations.setText(e.target.value)}
          onBlur={quantizations.commit}
          placeholder="fp16, int8"
          className="text-base md:text-sm"
        />
        <p className="text-xs text-muted-foreground">
          {t("routing.quantizationsHint")}
        </p>
      </div>

      {/* Max Price */}
      <div className="space-y-2">
        <Label>{t("routing.maxPrice")}</Label>
        <div className="grid grid-cols-2 gap-3">
          <div className="space-y-1">
            <Label className="text-xs text-muted-foreground">
              {t("routing.maxPricePrompt")}
            </Label>
            <Input
              type="number"
              step="0.000001"
              min="0"
              value={routing.max_price?.prompt ?? ""}
              onChange={(e) => {
                const v = e.target.value;
                const num = v === "" ? undefined : parseFloat(v);
                update({
                  max_price:
                    num !== undefined && !isNaN(num)
                      ? { ...routing.max_price, prompt: num }
                      : routing.max_price?.completion
                        ? { completion: routing.max_price.completion }
                        : null,
                });
              }}
              placeholder="0.00"
              className="text-base md:text-sm"
            />
          </div>
          <div className="space-y-1">
            <Label className="text-xs text-muted-foreground">
              {t("routing.maxPriceCompletion")}
            </Label>
            <Input
              type="number"
              step="0.000001"
              min="0"
              value={routing.max_price?.completion ?? ""}
              onChange={(e) => {
                const v = e.target.value;
                const num = v === "" ? undefined : parseFloat(v);
                update({
                  max_price:
                    num !== undefined && !isNaN(num)
                      ? { ...routing.max_price, completion: num }
                      : routing.max_price?.prompt
                        ? { prompt: routing.max_price.prompt }
                        : null,
                });
              }}
              placeholder="0.00"
              className="text-base md:text-sm"
            />
          </div>
        </div>
        <p className="text-xs text-muted-foreground">
          {t("routing.maxPriceHint")}
        </p>
      </div>
    </section>
  );
}
