import { Link } from "react-router";
import { ExternalLink } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ROUTES, route } from "@/lib/constants";
import { useTenants } from "@/hooks/use-tenants";

 
interface Props {
  data: Record<string, any> | undefined;
}

export function TtsSection({ data }: Props) {
  const { t } = useTranslation("config");
  const { currentTenantSlug } = useTenants();

  if (!data) return null;

  const provider = data.provider as string | undefined;
  const auto = data.auto as string | undefined;

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-center gap-2">
          <CardTitle className="text-base">{t("tts.title")}</CardTitle>
          <Badge variant={provider ? "default" : "secondary"}>
            {provider ? t("tts.configured") : t("tts.disabled")}
          </Badge>
        </div>
        <CardDescription>
          {provider
            ? t("tts.providerInfo", { provider, auto: auto ?? "off" })
            : t("tts.noProvider")}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Link
          to={route(currentTenantSlug, ROUTES.TTS)}
          className="inline-flex items-center gap-1.5 text-sm text-primary hover:underline"
        >
          {t("tts.manageLink")} <ExternalLink className="h-3.5 w-3.5" />
        </Link>
      </CardContent>
    </Card>
  );
}
