import { useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, Eraser, Pause, Play, RefreshCw, Search } from "lucide-react";
import { api } from "../../api/client";
import type { Container, Sample, Snapshot } from "../../api/types";
import { Meter, Sparkline } from "../../components/charts/Sparkline";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { Button } from "../../components/ui/Button";
import { Checkbox } from "../../components/ui/Checkbox";
import { Frame, SectionBar } from "../../components/ui/Frame";
import { ru } from "../../i18n/ru";
import { absolute } from "../../lib/format";
import { useSearchParam } from "../../lib/hooks";
import { bytes, latest, pct, rate, since, useMetrics, type ContainerSeries } from "../../lib/metrics";
import { AlertsPanel } from "./AlertsPanel";
import { MetricChart } from "./charts";

const iconProps = { size: 14, strokeWidth: 2, strokeLinecap: "square" as const };
const RANGES = [300000, 900000, 1800000];
const t = ru.mon;

type SortKey = "name" | "cpu" | "mem" | "net" | "disk" | "pids";

interface Row {
  id: string;
  series: ContainerSeries;
  last: Sample;
  container: Container | undefined;
}

interface Props {
  snapshot: Snapshot | undefined;
  onOpen: (c: Container) => void;
  onGoSettings: () => void;
  onGoCleanup: () => void;
}

function sortValue(r: Row, key: SortKey): number | string {
  switch (key) {
    case "name":
      return r.series.name;
    case "cpu":
      return r.last.cpu;
    case "mem":
      return r.last.mem;
    case "net":
      return r.last.netRx + r.last.netTx;
    case "disk":
      return r.last.blkR + r.last.blkW;
    case "pids":
      return r.last.pids;
  }
}

export function MonitoringPage({ snapshot, onOpen, onGoSettings, onGoCleanup }: Props) {
  const store = useMetrics();
  const host = useQuery({ queryKey: ["metrics-host"], queryFn: () => api.metricsHost(), refetchInterval: 30_000 });
  const [rangeParam, setRange] = useSearchParam("range", "900000");
  const range = RANGES.includes(Number(rangeParam)) ? Number(rangeParam) : 900000;
  const [query, setQuery] = useSearchParam("q", "");
  const [managedOnly, setManagedOnly] = useSearchParam("mine", "1");
  const [sort, setSort] = useState<{ key: SortKey; dir: 1 | -1 }>({ key: "cpu", dir: -1 });
  const [paused, setPaused] = useState(false);
  const frozen = useRef<{ totals: Sample[]; containers: Map<string, ContainerSeries>; at: number } | null>(null);

  if (paused && !frozen.current) {
    frozen.current = { totals: store.totals, containers: new Map(store.containers), at: latest(store.totals)?.t ?? Date.now() };
  }
  if (!paused && frozen.current) frozen.current = null;

  const totalsAll = frozen.current?.totals ?? store.totals;
  const containersAll = frozen.current?.containers ?? store.containers;
  const now = frozen.current?.at ?? latest(totalsAll)?.t ?? Date.now();
  const from = now - range;
  const totals = useMemo(() => since(totalsAll, from), [totalsAll, from]);

  const info = host.data?.info ?? null;
  const ncpu = info?.ncpu ?? 1;
  const memTotal = info?.memTotal ?? latest(totals)?.memLimit ?? 0;
  const last = latest(totals);

  const byId = useMemo(() => new Map((snapshot?.containers ?? []).map((c) => [c.id, c])), [snapshot]);

  const rows = useMemo(() => {
    const q = query.trim().toLowerCase();
    const out: Row[] = [];
    for (const [id, series] of containersAll) {
      const l = latest(series.samples);
      if (!l || l.t < now - 15_000) continue;
      const container = byId.get(id);
      if (managedOnly === "1" && container && !container.managed) continue;
      if (q && !`${series.name} ${series.project} ${series.service}`.toLowerCase().includes(q)) continue;
      out.push({ id, series, last: l, container });
    }
    out.sort((a, b) => {
      const va = sortValue(a, sort.key);
      const vb = sortValue(b, sort.key);
      const cmp = typeof va === "string" ? va.localeCompare(String(vb)) : va - (vb as number);
      return cmp * sort.dir;
    });
    return out;
  }, [containersAll, byId, query, managedOnly, sort, now]);

  const streamBadge = paused
    ? { tone: "warning", label: t.paused, glyph: "triangle" as const }
    : store.state === "live"
      ? { tone: "success", label: t.live, glyph: "square" as const }
      : store.state === "connecting"
        ? { tone: "progress", label: t.connecting, glyph: "ring" as const }
        : { tone: "danger", label: t.lost, glyph: "fault" as const };

  const header = (key: SortKey, label: string, className = "") => (
    <th scope="col" className={`th-sort ${className}`} aria-sort={sort.key === key ? (sort.dir === 1 ? "ascending" : "descending") : "none"}>
      <button
        type="button"
        title={t.sortBy(label)}
        onClick={() => setSort((s) => (s.key === key ? { key, dir: s.dir === 1 ? -1 : 1 } : { key, dir: key === "name" ? 1 : -1 }))}
      >
        {label}
        {sort.key === key && (sort.dir === 1 ? <ArrowUp {...iconProps} size={12} /> : <ArrowDown {...iconProps} size={12} />)}
      </button>
    </th>
  );

  const disk = host.data?.disk;
  const diskParts = disk
    ? [
        { label: t.images, value: disk.imagesSize, count: disk.images, note: disk.imagesUnused > 0 ? t.imagesUnused(bytes(disk.imagesUnused)) : "" },
        { label: t.volumes, value: disk.volumesSize, count: disk.volumes, note: "" },
        { label: t.containersRw, value: disk.containersSize, count: disk.containers, note: "" },
        { label: t.buildCache, value: disk.buildCacheSize, count: disk.buildCache, note: "" },
      ]
    : [];
  const diskTotal = diskParts.reduce((a, p) => a + p.value, 0);
  const diskMax = Math.max(1, ...diskParts.map((p) => p.value));

  return (
    <main className="page" id="main">
      <header className="page-head">
        <SectionBar title={t.title} jp={t.navJp} level={1} count={`${rows.length} UNITS`}>
          <div className="tabs" role="tablist" aria-label={t.range}>
            {RANGES.map((r) => (
              <button key={r} type="button" role="tab" aria-selected={range === r} className={`tab ${range === r ? "is-active" : ""}`} onClick={() => setRange(String(r))}>
                <span className="tab-inner">{t.ranges[r]}</span>
              </button>
            ))}
          </div>
          <Button variant="outline" icon={paused ? <Play {...iconProps} /> : <Pause {...iconProps} />} onClick={() => setPaused((p) => !p)} aria-pressed={paused}>
            {paused ? t.resume : t.pause}
          </Button>
        </SectionBar>
        <p className="page-summary">
          <span className={`badge badge-${streamBadge.tone}`}>
            <span className={streamBadge.glyph === "ring" ? "spin" : ""}>
              <StatusGlyph kind={streamBadge.glyph} size={10} />
            </span>
            {streamBadge.label}
          </span>
          {info && (
            <span className="mono micro">
              {t.engine(info.serverVersion, info.name)} · {info.operatingSystem} · {t.cores(info.ncpu)} · {bytes(info.memTotal)} · cgroup v{info.cgroupVersion}
            </span>
          )}
          {host.data && <span className="meta micro">{t.refreshEvery(Math.round(host.data.liveInterval / 1000))}</span>}
          {host.data?.infoError && <span className="tone-danger mono micro">{host.data.infoError}</span>}
        </p>
        {host.data && !host.data.background && (
          <p className="page-summary meta">
            {t.noBackground}{" "}
            <a
              href="/settings"
              onClick={(e) => {
                e.preventDefault();
                onGoSettings();
              }}
            >
              {t.toSettings}
            </a>
          </p>
        )}
      </header>

      <div className="mon-grid">
        <Frame className="mon-card" label={t.cpu}>
          <div className="mon-head">
            <div>
              <div className="mon-title">{t.cpu}</div>
              <div className="mon-value mono">{last ? pct(last.cpu / ncpu) : "—"}</div>
              <div className="meta">{last ? t.cpuOf((last.cpu / 100).toFixed(2), ncpu) : t.cpuHint}</div>
            </div>
            {last && <Meter value={last.cpu / ncpu} max={100} tone={last.cpu / ncpu > 90 ? "danger" : last.cpu / ncpu > 70 ? "warning" : undefined} label={t.cpu} />}
          </div>
          {totals.length > 1 ? <MetricChart kind="cpu" samples={totals} cpuDivisor={ncpu} /> : <p className="meta">{t.waiting}</p>}
        </Frame>

        <Frame className="mon-card" label={t.mem}>
          <div className="mon-head">
            <div>
              <div className="mon-title">{t.mem}</div>
              <div className="mon-value mono">{last ? bytes(last.mem) : "—"}</div>
              <div className="meta">{last && memTotal ? `${t.memOf(bytes(last.mem), bytes(memTotal))} · ${pct((last.mem / memTotal) * 100)}` : ""}</div>
            </div>
            {last && memTotal > 0 && (
              <Meter value={last.mem} max={memTotal} tone={last.mem / memTotal > 0.9 ? "danger" : last.mem / memTotal > 0.75 ? "warning" : undefined} label={t.mem} />
            )}
          </div>
          {totals.length > 1 ? <MetricChart kind="mem" samples={totals} /> : <p className="meta">{t.waiting}</p>}
        </Frame>

        <Frame className="mon-card" label={t.net}>
          <div className="mon-head">
            <div>
              <div className="mon-title">{t.net}</div>
              <div className="mon-value mono">{last ? `↓ ${rate(last.netRx)}` : "—"}</div>
              <div className="meta mono">{last ? `↑ ${rate(last.netTx)}` : ""}</div>
            </div>
          </div>
          {totals.length > 1 ? <MetricChart kind="net" samples={totals} /> : <p className="meta">{t.waiting}</p>}
        </Frame>

        <Frame className="mon-card" label={t.disk}>
          <div className="mon-head">
            <div>
              <div className="mon-title">{t.disk}</div>
              <div className="mon-value mono">{last ? `R ${rate(last.blkR)}` : "—"}</div>
              <div className="meta mono">{last ? `W ${rate(last.blkW)}` : ""}</div>
            </div>
          </div>
          {totals.length > 1 ? <MetricChart kind="disk" samples={totals} /> : <p className="meta">{t.waiting}</p>}
        </Frame>
      </div>

      <div className="toolbar">
        <label className="search">
          <Search {...iconProps} aria-hidden />
          <span className="visually-hidden">{t.search}</span>
          <input type="search" value={query} placeholder={t.search} onChange={(e) => setQuery(e.target.value)} />
        </label>
        <Checkbox checked={managedOnly === "1"} onChange={(v) => setManagedOnly(v ? "1" : "0")}>
          {t.onlyManaged}
        </Checkbox>
      </div>

      <Frame flush label={t.containers}>
        {rows.length === 0 ? (
          <div className="empty">
            <StatusGlyph kind="dashed" size={32} />
            <div className="empty-code">{ru.empty.noData}</div>
            <p>{store.state === "live" ? t.noData : t.waiting}</p>
          </div>
        ) : (
          <div className="table-scroll">
            <table className="table table-mon">
              <thead>
                <tr>
                  {header("name", t.col.name)}
                  {header("cpu", t.col.cpu, "num")}
                  <th scope="col">{t.col.cpuTrend}</th>
                  {header("mem", t.col.mem, "num")}
                  <th scope="col">{t.col.memTrend}</th>
                  {header("net", t.col.net, "num")}
                  {header("disk", t.col.disk, "num")}
                  {header("pids", t.col.pids, "num")}
                  <th scope="col" className="th-actions">
                    <span className="visually-hidden">{ru.columns.actions}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => {
                  const win = since(r.series.samples, from);
                  const memRatio = r.last.memLimit > 0 ? r.last.mem / r.last.memLimit : 0;
                  return (
                    <tr key={r.id} className="row">
                      <td data-label={t.col.name}>
                        <div className="mono mon-name">{r.series.name}</div>
                        <div className="meta micro">
                          {r.series.project} · {r.series.service}
                          {r.container?.disabled && <span className="tone-warning"> · {ru.disabled.badge}</span>}
                        </div>
                      </td>
                      <td data-label={t.col.cpu} className="num mono">
                        {pct(r.last.cpu)}
                        <Meter value={r.last.cpu} max={ncpu * 100} label={t.col.cpu} tone={r.last.cpu > 90 ? "warning" : undefined} />
                      </td>
                      <td data-label={t.col.cpuTrend}>
                        <Sparkline values={win.map((s) => s.cpu)} label={`${t.col.cpuTrend}: ${r.series.name}`} max={5} />
                      </td>
                      <td data-label={t.col.mem} className="num mono">
                        {bytes(r.last.mem)}
                        <Meter
                          value={r.last.mem}
                          max={r.last.memLimit || memTotal}
                          tone={memRatio > 0.9 ? "danger" : memRatio > 0.75 ? "warning" : undefined}
                          label={t.col.mem}
                        />
                      </td>
                      <td data-label={t.col.memTrend}>
                        <Sparkline values={win.map((s) => s.mem)} fit label={`${t.col.memTrend}: ${r.series.name}`} />
                      </td>
                      <td data-label={t.col.net} className="num mono micro">
                        <div>↓ {rate(r.last.netRx)}</div>
                        <div className="meta">↑ {rate(r.last.netTx)}</div>
                      </td>
                      <td data-label={t.col.disk} className="num mono micro">
                        <div>R {rate(r.last.blkR)}</div>
                        <div className="meta">W {rate(r.last.blkW)}</div>
                      </td>
                      <td data-label={t.col.pids} className="num mono">
                        {r.last.pids}
                      </td>
                      <td className="cell-actions">
                        {r.container && (
                          <Button size="sm" variant="ghost" onClick={() => onOpen(r.container!)}>
                            {t.open}
                          </Button>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Frame>

      <Frame label={t.storage}>
        <div className="settings-block">
          <SectionBar title={t.storage} jp={t.storageJp} count={disk ? t.totalDisk(bytes(diskTotal)) : ""}>
            <Button size="sm" variant="ghost" icon={<RefreshCw {...iconProps} />} loading={host.isFetching} onClick={() => void api.metricsHost(true).then(() => host.refetch())}>
              {t.refresh}
            </Button>
          </SectionBar>
          {host.data?.diskError && <p className="tone-danger mono micro">{host.data.diskError}</p>}
          {diskParts.map((p) => (
            <div key={p.label} className="meter-row">
              <span className="meter-row-label">{p.label}</span>
              <Meter value={p.value} max={diskMax} label={p.label} />
              <span className="mono meter-row-value">{bytes(p.value)}</span>
              <span className="meta micro">
                {t.count(p.count)}
                {p.note && ` · ${p.note}`}
              </span>
            </div>
          ))}
          {host.data?.diskAt && <p className="meta micro">{t.measuredAt(absolute(host.data.diskAt))}</p>}
          <div className="mon-prune">
            <p className="meta micro">{t.pruneHint}</p>
            <Button size="sm" variant="outline" icon={<Eraser {...iconProps} />} onClick={onGoCleanup}>
              {t.toCleanup}
            </Button>
          </div>
        </div>
      </Frame>
      <AlertsPanel containers={snapshot?.containers ?? []} onOpen={onOpen} />
    </main>
  );
}
