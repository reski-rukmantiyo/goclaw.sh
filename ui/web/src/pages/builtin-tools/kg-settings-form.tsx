import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { useTranslation } from "react-i18next";
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  verticalListSortingStrategy,
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Loader2, GripVertical, Plus, Trash2 } from "lucide-react";
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
import { Combobox } from "@/components/ui/combobox";
import { ProviderModelSelect } from "@/components/shared/provider-model-select";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useProviderModels } from "@/pages/providers/hooks/use-provider-models";
import { uniqueId } from "@/lib/utils";

interface FallbackEntry {
  id: string;
  provider: string;
  model: string;
}

interface KGSettings {
  extract_on_memory_write: boolean;
  extraction_provider: string;
  extraction_model: string;
  min_confidence: number;
  extraction_timeout_sec: number;
  extraction_fallback_providers: { provider: string; model: string }[];
}

const defaultSettings: KGSettings = {
  extract_on_memory_write: false,
  extraction_provider: "",
  extraction_model: "",
  min_confidence: 0.75,
  extraction_timeout_sec: 0,
  extraction_fallback_providers: [],
};

interface Props {
  initialSettings: Record<string, unknown>;
  onSave: (settings: Record<string, unknown>) => Promise<void>;
  onCancel: () => void;
}

/** Single sortable fallback provider card. */
function SortableFallbackCard({
  entry,
  index,
  enabledProviders,
  onUpdate,
  onRemove,
  portalRef,
}: {
  entry: FallbackEntry;
  index: number;
  enabledProviders: ReturnType<typeof useProviders>["providers"];
  onUpdate: (id: string, patch: Partial<FallbackEntry>) => void;
  onRemove: (id: string) => void;
  portalRef: React.RefObject<HTMLDivElement | null>;
}) {
  const { t } = useTranslation("tools");
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: entry.id });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
  };

  const selectedProvider = enabledProviders.find((p) => p.name === entry.provider);
  const { models, loading: modelsLoading } = useProviderModels(selectedProvider?.id);

  return (
    <div ref={setNodeRef} style={style} className="border rounded-lg bg-card">
      <div className="flex items-center gap-2 px-3 pt-3 pb-1">
        <button
          type="button"
          className="cursor-grab text-muted-foreground hover:text-foreground shrink-0"
          {...attributes}
          {...listeners}
        >
          <GripVertical className="size-4" />
        </button>
        <span className="text-xs text-muted-foreground font-mono shrink-0">#{index + 1}</span>
        <span className="text-sm font-medium truncate">
          {selectedProvider?.display_name || entry.provider || t("builtin.kgSettings.selectProvider")}
        </span>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="ml-auto h-7 w-7 p-0 shrink-0 text-muted-foreground hover:text-destructive"
          onClick={() => onRemove(entry.id)}
        >
          <Trash2 className="size-3.5" />
        </Button>
      </div>
      <div className="grid grid-cols-1 gap-2 px-3 py-1.5 pb-3 sm:grid-cols-2">
        <div className="space-y-1">
          <Label className="text-xs text-muted-foreground">{t("builtin.kgSettings.fallbackProvider")}</Label>
          <Select
            value={entry.provider}
            onValueChange={(v) => onUpdate(entry.id, { provider: v, model: "" })}
          >
            <SelectTrigger className="h-8 text-sm">
              <SelectValue placeholder={t("builtin.kgSettings.selectProvider")} />
            </SelectTrigger>
            <SelectContent>
              {enabledProviders.map((p) => (
                <SelectItem key={p.id} value={p.name}>
                  {p.display_name || p.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1">
          <Label className="text-xs text-muted-foreground">{t("builtin.kgSettings.fallbackModel")}</Label>
          <Combobox
            value={entry.model}
            onChange={(v) => onUpdate(entry.id, { model: v })}
            options={models.map((m) => ({ value: m.id, label: m.name ?? m.id }))}
            placeholder={modelsLoading ? "..." : t("builtin.kgSettings.selectModel")}
            className="h-8 text-sm"
            portalContainer={portalRef}
          />
        </div>
      </div>
    </div>
  );
}

export function KGSettingsForm({ initialSettings, onSave, onCancel }: Props) {
  const { t } = useTranslation("tools");
  const { providers } = useProviders();
  const enabledProviders = useMemo(() => providers.filter((p) => p.enabled), [providers]);
  const portalRef = useRef<HTMLDivElement | null>(null);

  const [settings, setSettings] = useState<KGSettings>(defaultSettings);
  const [fallbacks, setFallbacks] = useState<FallbackEntry[]>([]);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    const raw = {
      ...defaultSettings,
      ...initialSettings,
      min_confidence: Number(initialSettings.min_confidence) || defaultSettings.min_confidence,
    } as KGSettings;

    setSettings(raw);

    const fb = (raw.extraction_fallback_providers ?? []).map((f) => ({
      id: uniqueId(),
      provider: f.provider ?? "",
      model: f.model ?? "",
    }));
    setFallbacks(fb);
  }, [initialSettings]);

  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const handleDragEnd = useCallback((event: DragEndEvent) => {
    const { active, over } = event;
    if (over && active.id !== over.id) {
      setFallbacks((prev) => {
        const oldIndex = prev.findIndex((e) => e.id === active.id);
        const newIndex = prev.findIndex((e) => e.id === over.id);
        return arrayMove(prev, oldIndex, newIndex);
      });
    }
  }, []);

  const handleFallbackUpdate = useCallback((id: string, patch: Partial<FallbackEntry>) => {
    setFallbacks((prev) => prev.map((e) => (e.id === id ? { ...e, ...patch } : e)));
  }, []);

  const handleFallbackRemove = useCallback((id: string) => {
    setFallbacks((prev) => prev.filter((e) => e.id !== id));
  }, []);

  const handleAddFallback = () => {
    setFallbacks((prev) => [...prev, { id: uniqueId(), provider: "", model: "" }]);
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      const payload = {
        ...settings,
        extraction_fallback_providers: fallbacks.map(({ provider, model }) => ({ provider, model })),
      };
      await onSave(payload as unknown as Record<string, unknown>);
    } catch {
      // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  return (
    <div ref={portalRef} className="relative">
      <DialogHeader>
        <DialogTitle>{t("builtin.kgSettings.title")}</DialogTitle>
        <DialogDescription>
          {t("builtin.kgSettings.description")}
        </DialogDescription>
      </DialogHeader>

      <div className="space-y-4 py-2 max-h-[70vh] overflow-y-auto pr-1">
        <ProviderModelSelect
          provider={settings.extraction_provider}
          onProviderChange={(v) => setSettings((s) => ({ ...s, extraction_provider: v }))}
          model={settings.extraction_model}
          onModelChange={(v) => setSettings((s) => ({ ...s, extraction_model: v }))}
          providerLabel={t("builtin.kgSettings.extractionProvider")}
          modelLabel={t("builtin.kgSettings.extractionModel")}
          providerTip={t("builtin.kgSettings.providerTip")}
          modelTip={t("builtin.kgSettings.modelTip")}
        />

        <div className="grid gap-1.5">
          <Label htmlFor="kg-min-conf" className="text-sm">{t("builtin.kgSettings.minConfidence")}</Label>
          <Input
            id="kg-min-conf"
            type="number"
            min={0}
            max={1}
            step={0.05}
            value={settings.min_confidence}
            onChange={(e) => setSettings((s) => ({ ...s, min_confidence: Number(e.target.value) || 0.75 }))}
            className="max-w-[120px]"
          />
          <p className="text-xs text-muted-foreground">
            {t("builtin.kgSettings.minConfidenceHint")}
          </p>
        </div>

        <div className="grid gap-1.5">
          <Label htmlFor="kg-timeout" className="text-sm">{t("builtin.kgSettings.extractionTimeout")}</Label>
          <Input
            id="kg-timeout"
            type="number"
            min={0}
            max={300}
            step={10}
            value={settings.extraction_timeout_sec}
            onChange={(e) => setSettings((s) => ({ ...s, extraction_timeout_sec: Number(e.target.value) || 0 }))}
            className="max-w-[120px]"
          />
          <p className="text-xs text-muted-foreground">
            {t("builtin.kgSettings.extractionTimeoutHint")}
          </p>
        </div>

        <div className="flex items-center justify-between rounded-md border p-3">
          <div>
            <Label htmlFor="kg-auto-extract" className="text-sm font-medium">{t("builtin.kgSettings.autoExtract")}</Label>
            <p className="text-xs text-muted-foreground mt-0.5">
              {t("builtin.kgSettings.autoExtractHint")}
            </p>
          </div>
          <Switch
            id="kg-auto-extract"
            checked={settings.extract_on_memory_write}
            onCheckedChange={(v) => setSettings((s) => ({ ...s, extract_on_memory_write: v }))}
          />
        </div>

        {/* Fallback providers section */}
        <div className="space-y-2 pt-2">
          <div>
            <Label className="text-sm font-medium">{t("builtin.kgSettings.fallbackTitle")}</Label>
            <p className="text-xs text-muted-foreground mt-0.5">
              {t("builtin.kgSettings.fallbackHint")}
            </p>
          </div>

          <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
            <SortableContext items={fallbacks.map((e) => e.id)} strategy={verticalListSortingStrategy}>
              {fallbacks.map((entry, index) => (
                <SortableFallbackCard
                  key={entry.id}
                  entry={entry}
                  index={index}
                  enabledProviders={enabledProviders}
                  onUpdate={handleFallbackUpdate}
                  onRemove={handleFallbackRemove}
                  portalRef={portalRef}
                />
              ))}
            </SortableContext>
          </DndContext>

          {fallbacks.length === 0 && (
            <p className="text-sm text-muted-foreground text-center py-3">
              {t("builtin.kgSettings.noFallbacks")}
            </p>
          )}

          <Button type="button" variant="outline" size="sm" className="w-full" onClick={handleAddFallback}>
            <Plus className="size-3.5 mr-1.5" />
            {t("builtin.kgSettings.addFallback")}
          </Button>
        </div>
      </div>

      <DialogFooter>
        <Button variant="outline" onClick={onCancel}>{t("builtin.kgSettings.cancel")}</Button>
        <Button onClick={handleSave} disabled={saving}>
          {saving && <Loader2 className="h-4 w-4 animate-spin" />}
          {saving ? t("builtin.kgSettings.saving") : t("builtin.kgSettings.save")}
        </Button>
      </DialogFooter>
    </div>
  );
}
