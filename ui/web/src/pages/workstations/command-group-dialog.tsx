import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import type { CommandGroup } from "./hooks/use-command-groups";

interface CommandGroupDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (params: { name: string; description: string; patterns: string[] }) => Promise<void>;
  group?: CommandGroup | null;
}

export function CommandGroupDialog({ open, onOpenChange, onSubmit, group }: CommandGroupDialogProps) {
  const { t } = useTranslation("workstations");
  const isEdit = !!group;

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [patternsText, setPatternsText] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (group) {
      setName(group.name);
      setDescription(group.description || "");
      setPatternsText(group.patterns?.join("\n") || "");
    } else {
      setName("");
      setDescription("");
      setPatternsText("");
    }
  }, [group, open]);

  const handleSubmit = async () => {
    if (!name.trim()) return;
    const patterns = patternsText
      .split("\n")
      .map((p) => p.trim())
      .filter((p) => p.length > 0);
    setSubmitting(true);
    try {
      await onSubmit({ name: name.trim(), description: description.trim(), patterns });
      onOpenChange(false);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {isEdit ? t("commandGroups.dialog.editTitle") : t("commandGroups.dialog.createTitle")}
          </DialogTitle>
          <DialogDescription />
        </DialogHeader>
        <div className="space-y-4 py-2">
          <div className="space-y-1.5">
            <Label htmlFor="cg-name">{t("commandGroups.dialog.nameLabel")}</Label>
            <Input
              id="cg-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("commandGroups.dialog.namePlaceholder")}
              className="text-base md:text-sm"
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="cg-desc">{t("commandGroups.dialog.descriptionLabel")}</Label>
            <Input
              id="cg-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t("commandGroups.dialog.descriptionPlaceholder")}
              className="text-base md:text-sm"
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="cg-patterns">{t("commandGroups.dialog.patternsLabel")}</Label>
            <Textarea
              id="cg-patterns"
              value={patternsText}
              onChange={(e) => setPatternsText(e.target.value)}
              placeholder={t("commandGroups.dialog.patternsHint")}
              rows={6}
              className="text-base md:text-sm font-mono"
            />
            <p className="text-xs text-muted-foreground">{t("commandGroups.dialog.patternsHint")}</p>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("commandGroups.dialog.cancel")}
          </Button>
          <Button onClick={handleSubmit} disabled={!name.trim() || submitting}>
            {t("commandGroups.dialog.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
