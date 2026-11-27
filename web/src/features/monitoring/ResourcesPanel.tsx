import { useQuery } from "@tanstack/react-query";
import { api } from "../../api/client";
import type { Container } from "../../api/types";
import { SectionBar } from "../../components/ui/Frame";
import { ru } from "../../i18n/ru";
import { bytes, latest, pct, rate, useMetrics } from "../../lib/metrics";
import { MetricChart } from "./charts";

export function ResourcesPanel({ container }: { container: Container }) {
  const store = useMetrics();
  const host = useQuery({ queryKey: ["metrics-host"], queryFn: () => api.metricsHost(), staleTime: 60_000 });
  const hostMem = host.data?.info?.memTotal ?? 0;
  const series = store.containers.get(container.id);
  const samples = series?.samples ?? [];
  const last = latest(samples);

  if (container.state !== "running" && samples.length === 0) {
    return (
      <div className="empty">
        <div className="empty-code">{ru.empty.noData}</div>
        <p>{ru.mon.notRunning}</p>
      </div>
    );
  }
  if (samples.length < 2) {
    return <p className="meta">{ru.mon.waiting}</p>;
  }

  const limit = last?.memLimit ?? 0;

  return (
    <div className="respanel">
      <section className="respanel-block">
        <SectionBar title={ru.mon.cpu} count={last ? pct(last.cpu) : ""} />
        <p className="meta micro">{ru.mon.perCore}</p>
        <MetricChart kind="cpu" samples={samples} height={150} />
      </section>
      <section className="respanel-block">
        <SectionBar title={ru.mon.mem} count={last ? ru.mon.memOf(bytes(last.mem), bytes(limit)) : ""} />
        <MetricChart kind="mem" samples={samples} height={150} memLimit={limit > 0 && hostMem > 0 && limit < hostMem * 0.95 ? limit : undefined} />
      </section>
      <section className="respanel-block">
        <SectionBar title={ru.mon.net} count={last ? `↓ ${rate(last.netRx)} · ↑ ${rate(last.netTx)}` : ""} />
        <MetricChart kind="net" samples={samples} height={150} />
      </section>
      <section className="respanel-block">
        <SectionBar title={ru.mon.disk} count={last ? `R ${rate(last.blkR)} · W ${rate(last.blkW)}` : ""} />
        <MetricChart kind="disk" samples={samples} height={150} />
      </section>
      {last && <p className="meta mono micro">PIDs {last.pids}</p>}
    </div>
  );
}
