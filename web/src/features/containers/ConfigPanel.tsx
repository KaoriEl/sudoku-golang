import { useQuery } from "@tanstack/react-query";
import { Copy, Eye, EyeOff } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { ApiError, api } from "../../api/client";
import type { Container } from "../../api/types";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { Button } from "../../components/ui/Button";
import { SectionBar } from "../../components/ui/Frame";
import { ru } from "../../i18n/ru";
import { rf } from "../../i18n/ru.features";

const t = rf.config;
const iconProps = { size: 14, strokeWidth: 2, strokeLinecap: "square" as const };
const secretKey = /(PASSWORD|PASSWD|SECRET|TOKEN|KEY|CREDENTIAL)[A-Z0-9_]*\s*[:=]/i;
const REVEAL_MS = 30_000;

function mask(yaml: string): string {
  return yaml
    .split("\n")
    .map((line) => {
      if (!secretKey.test(line)) return line;
      const i = line.search(/[:=]/);
      const value = line.slice(i + 1).trim();
      return value ? `${line.slice(0, i + 1)} ${"█".repeat(8)}` : line;
    })
    .join("\n");
}

export function ConfigPanel({ container }: { container: Container }) {
  const q = useQuery({ queryKey: ["service-config", container.id], queryFn: () => api.serviceConfig(container.id), staleTime: 15_000 });
  const [raw, setRaw] = useState(false);
  const [reveal, setReveal] = useState(false);

  useEffect(() => {
    if (!reveal) return;
    const id = window.setTimeout(() => setReveal(false), REVEAL_MS);
    return () => window.clearTimeout(id);
  }, [reveal]);

  const cfg = q.data;
  const shown = useMemo(() => {
    const text = (raw ? cfg?.rawYaml : cfg?.yaml) ?? "";
    return reveal ? text : mask(text);
  }, [cfg, raw, reveal]);

  if (q.isLoading) return <div className="skeleton-rows"><div className="skeleton-row" /><div className="skeleton-row" /></div>;
  if (q.isError || !cfg) {
    return (
      <div className="empty">
        <StatusGlyph kind="fault" size={24} />
        <p className="mono">{q.error instanceof ApiError ? q.error.message : String(q.error)}</p>
        <Button variant="outline" size="sm" onClick={() => void q.refetch()}>
          {ru.error.retry}
        </Button>
      </div>
    );
  }

  return (
    <div className="config-panel">
      {cfg.error && (
        <div className="notice notice-warning">
          <StatusGlyph kind="fault" size={12} />
          <span className="mono micro">{cfg.error}</span>
        </div>
      )}
      <SectionBar title={t.variables} count={String(cfg.variables.length)} />
      {cfg.variables.length === 0 ? (
        <p className="meta">{t.noVars}</p>
      ) : (
        <table className="table table-vars">
          <tbody>
            {cfg.variables.map((v) => (
              <tr key={v.name} className={v.set ? "" : "is-fault"}>
                <td className="mono">{"${" + v.name + "}"}</td>
                <td>
                  {v.set ? (
                    <span className="badge badge-success">
                      <StatusGlyph kind="square" size={8} />
                      {t.set}
                    </span>
                  ) : (
                    <span className="badge badge-danger">
                      <StatusGlyph kind="fault" size={8} />
                      {t.unset}
                    </span>
                  )}
                </td>
                <td className="meta">{[v.default ? t.def(v.default) : "", v.required ? t.required : ""].filter(Boolean).join(" · ")}</td>
                <td className="num mono micro">{t.uses(v.uses)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <SectionBar title={raw ? t.raw : t.resolved}>
        <Button size="sm" variant={raw ? "outline" : "ghost"} aria-pressed={raw} onClick={() => setRaw((r) => !r)}>
          {t.raw}
        </Button>
        <Button size="sm" variant="ghost" icon={reveal ? <EyeOff {...iconProps} /> : <Eye {...iconProps} />} aria-pressed={reveal} onClick={() => setReveal((r) => !r)}>
          {reveal ? t.hide : t.reveal}
        </Button>
        <Button size="sm" variant="ghost" iconOnly icon={<Copy {...iconProps} />} title={ru.copy} onClick={() => void navigator.clipboard.writeText(shown).catch(() => undefined)}>
          {ru.copy}
        </Button>
      </SectionBar>
      <pre className="code-block mono">{shown || "—"}</pre>

      {cfg.warnings.length > 0 && (
        <>
          <SectionBar title={t.warnings} count={String(cfg.warnings.length)} />
          <ul className="warn-list mono micro">
            {cfg.warnings.map((w) => (
              <li key={w} className="tone-warning">
                {w}
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
}
