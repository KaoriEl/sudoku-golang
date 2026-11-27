import { useMemo } from "react";
import type { Sample } from "../../api/types";
import { TimeChart, type ChartSeries } from "../../components/charts/TimeChart";
import { ru } from "../../i18n/ru";
import { bytes, metricsStore, pct, rate } from "../../lib/metrics";

export type MetricKind = "cpu" | "mem" | "net" | "disk";

interface Props {
  kind: MetricKind;
  samples: Sample[];
  height?: number;
  cpuDivisor?: number;
  memLimit?: number;
}

function withGaps(samples: Sample[], gapMs: number): (Sample | null)[] {
  const out: (Sample | null)[] = [];
  for (let i = 0; i < samples.length; i++) {
    const s = samples[i]!;
    const prev = samples[i - 1];
    if (prev && s.t - prev.t > gapMs) out.push(null);
    out.push(s);
  }
  return out;
}

export function MetricChart({ kind, samples: raw, height, cpuDivisor = 1, memLimit }: Props) {
  const gapMs = metricsStore.gapMs;
  const gapped = useMemo(() => withGaps(raw, gapMs), [raw, gapMs]);
  const times = useMemo(() => {
    const ts: number[] = [];
    gapped.forEach((s, i) => ts.push(s ? s.t : (gapped[i - 1]?.t ?? 0) + 1));
    return ts;
  }, [gapped]);
  const samples = gapped;
  const series = useMemo<ChartSeries[]>(() => {
    switch (kind) {
      case "cpu":
        return [{ label: ru.mon.cpu, colorVar: "--chart-1", values: samples.map((s) => (s ? s.cpu / cpuDivisor : null)) }];
      case "mem":
        return [{ label: ru.mon.mem, colorVar: "--chart-1", values: samples.map((s) => (s ? s.mem : null)) }];
      case "net":
        return [
          { label: ru.mon.rx, colorVar: "--chart-1", values: samples.map((s) => (s ? s.netRx : null)) },
          { label: ru.mon.tx, colorVar: "--chart-2", values: samples.map((s) => (s ? s.netTx : null)), dashed: true },
        ];
      case "disk":
        return [
          { label: ru.mon.read, colorVar: "--chart-1", values: samples.map((s) => (s ? s.blkR : null)) },
          { label: ru.mon.write, colorVar: "--chart-2", values: samples.map((s) => (s ? s.blkW : null)), dashed: true },
        ];
    }
  }, [kind, samples, cpuDivisor]);

  const format = kind === "cpu" ? pct : kind === "mem" ? (v: number) => bytes(v) : rate;
  const yMin = kind === "cpu" ? 5 : kind === "mem" ? 64 * 1024 * 1024 : 1024;
  const refLine = kind === "mem" && memLimit ? { value: memLimit, label: `${ru.mon.limit} ${bytes(memLimit)}` } : undefined;
  const label = { cpu: ru.mon.cpu, mem: ru.mon.mem, net: ru.mon.net, disk: ru.mon.disk }[kind];

  return <TimeChart times={times} series={series} format={format} height={height} yMin={yMin} label={label} refLine={refLine} />;
}
