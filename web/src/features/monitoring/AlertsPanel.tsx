import { useQuery } from "@tanstack/react-query";
import { api } from "../../api/client";
import type { Container } from "../../api/types";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { Button } from "../../components/ui/Button";
import { Frame, SectionBar } from "../../components/ui/Frame";
import { rf } from "../../i18n/ru.features";
import { absolute, ago } from "../../lib/format";
import { useNow } from "../../lib/hooks";

const t = rf.alerts;

export function AlertsPanel({ containers, onOpen }: { containers: Container[]; onOpen: (c: Container) => void }) {
  const now = useNow(30_000);
  const q = useQuery({ queryKey: ["alerts"], queryFn: api.alerts, staleTime: 60_000 });
  const list = q.data ?? [];
  return (
    <Frame flush label={t.title}>
      <div className="frame-pad">
        <SectionBar title={t.title} jp={t.titleJp} count={String(list.length)} />
      </div>
      {list.length === 0 ? (
        <p className="meta frame-pad">{t.empty}</p>
      ) : (
        <ul className="alert-list">
          {list.map((a) => {
            const c = containers.find((x) => x.name === a.container);
            return (
              <li key={a.id} className="alert-item tone-danger">
                <StatusGlyph kind={a.kind === "unhealthy" ? "triangle" : "fault"} size={10} label={t.kinds[a.kind]} />
                <span className="badge badge-danger">{t.kinds[a.kind] ?? a.kind}</span>
                <span className="mono">{a.container}</span>
                {a.exitCode && a.kind === "crash" && <span className="meta micro">{t.exit(a.exitCode)}</span>}
                <span className="meta micro" title={absolute(a.at)}>
                  {ago(a.at, now)}
                </span>
                {c && (
                  <Button size="sm" variant="ghost" onClick={() => onOpen(c)}>
                    {t.open}
                  </Button>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </Frame>
  );
}
