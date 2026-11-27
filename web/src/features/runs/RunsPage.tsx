import { useQuery } from "@tanstack/react-query";
import { ScrollText } from "lucide-react";
import { ApiError, api } from "../../api/client";
import type { RunSummary } from "../../api/types";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { Button } from "../../components/ui/Button";
import { Frame, SectionBar } from "../../components/ui/Frame";
import { ru } from "../../i18n/ru";
import { rf, runActionLabel } from "../../i18n/ru.features";
import { absolute, ago, duration } from "../../lib/format";
import { useNow } from "../../lib/hooks";

const t = rf.runs;
const iconProps = { size: 14, strokeWidth: 2, strokeLinecap: "square" as const };
const tone: Record<string, string> = { running: "progress", success: "success", partial: "warning", failed: "danger" };
const glyph: Record<string, "ring" | "square" | "triangle" | "fault"> = { running: "ring", success: "square", partial: "triangle", failed: "fault" };

export function RunsPage({ onOpen }: { onOpen: (r: RunSummary) => void }) {
  const now = useNow(30_000);
  const q = useQuery({ queryKey: ["runs"], queryFn: api.runs, refetchInterval: 15_000 });
  const runs = q.data ?? [];

  return (
    <main className="page" id="main">
      <header className="page-head">
        <SectionBar title={t.title} jp={t.titleJp} level={1} count={`${runs.length} RUNS`} />
      </header>
      <Frame flush label={t.title}>
        {q.isLoading ? (
          <div className="skeleton-rows">
            {Array.from({ length: 6 }, (_, i) => (
              <div key={i} className="skeleton-row" />
            ))}
          </div>
        ) : q.isError ? (
          <div className="empty">
            <StatusGlyph kind="fault" size={32} />
            <p className="mono">{q.error instanceof ApiError ? q.error.message : String(q.error)}</p>
            <Button variant="outline" onClick={() => void q.refetch()}>
              {ru.error.retry}
            </Button>
          </div>
        ) : runs.length === 0 ? (
          <div className="empty">
            <StatusGlyph kind="dashed" size={32} />
            <p>{t.empty}</p>
          </div>
        ) : (
          <div className="table-scroll">
            <table className="table table-runs">
              <thead>
                <tr>
                  <th>{t.columns.when}</th>
                  <th>{t.columns.action}</th>
                  <th>{t.columns.target}</th>
                  <th>{t.columns.status}</th>
                  <th className="num">{t.columns.steps}</th>
                  <th className="num">{t.columns.duration}</th>
                  <th className="th-actions" />
                </tr>
              </thead>
              <tbody>
                {runs.map((r) => (
                  <tr key={r.id} className={r.status === "failed" ? "is-fault" : ""}>
                    <td title={absolute(r.startedAt)}>{ago(r.startedAt, now)}</td>
                    <td>{runActionLabel(r.action)}</td>
                    <td className="mono micro">{r.target || "—"}</td>
                    <td>
                      <span className={`badge badge-${tone[r.status] ?? "neutral"}`}>
                        <span className={r.status === "running" ? "spin" : ""}>
                          <StatusGlyph kind={glyph[r.status] ?? "outline"} size={10} />
                        </span>
                        {ru.run.status[r.status]}
                      </span>
                      {r.failed > 0 && <span className="meta micro"> · {t.failed(r.failed)}</span>}
                    </td>
                    <td className="num mono">{r.steps}</td>
                    <td className="num mono micro">{duration(r.startedAt, r.finishedAt, now)}</td>
                    <td className="row-actions">
                      <Button size="sm" variant="ghost" icon={<ScrollText {...iconProps} />} onClick={() => onOpen(r)}>
                        {t.open}
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Frame>
    </main>
  );
}
