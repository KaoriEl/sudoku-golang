import { useEffect, useRef, useState } from "react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";

export interface ChartSeries {
  label: string;
  colorVar: "--chart-1" | "--chart-2";
  values: (number | null)[];
  dashed?: boolean;
}

interface Props {
  times: number[];
  series: ChartSeries[];
  format: (v: number) => string;
  height?: number;
  yMin?: number;
  label: string;
  refLine?: { value: number; label: string };
}

function cssVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function withAlpha(hex: string, alpha: number): string {
  const m = /^#([0-9a-f]{6})$/i.exec(hex);
  if (!m) return hex;
  const n = parseInt(m[1]!, 16);
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${alpha})`;
}

function useThemeVersion(): number {
  const [v, setV] = useState(0);
  useEffect(() => {
    const mo = new MutationObserver(() => setV((x) => x + 1));
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const onMq = () => setV((x) => x + 1);
    mq.addEventListener("change", onMq);
    return () => {
      mo.disconnect();
      mq.removeEventListener("change", onMq);
    };
  }, []);
  return v;
}

const timeFmt = new Intl.DateTimeFormat("ru-RU", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
const axisTimeFmt = new Intl.DateTimeFormat("ru-RU", { hour: "2-digit", minute: "2-digit" });

export function TimeChart({ times, series, format, height = 180, yMin = 1, label, refLine }: Props) {
  const host = useRef<HTMLDivElement>(null);
  const plot = useRef<uPlot | null>(null);
  const themeVersion = useThemeVersion();
  const fmtRef = useRef(format);
  fmtRef.current = format;
  const refRef = useRef(refLine);
  refRef.current = refLine;
  const shape = series.map((s) => `${s.label}:${s.colorVar}:${s.dashed ? 1 : 0}`).join("|");

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    const muted = cssVar("--text-muted");
    const line = cssVar("--line");
    const font = '11px "JetBrains Mono", ui-monospace, monospace';
    const single = series.length === 1;

    const opts: uPlot.Options = {
      width: el.clientWidth || 400,
      height,
      padding: [8, 8, 0, 0],
      cursor: { drag: { x: false, y: false }, points: { size: 8, width: 2 }, y: false },
      legend: { show: true, live: true },
      scales: {
        x: { time: true },
        y: {
          range: (_u, _min, max) => {
            const ref = refRef.current?.value ?? 0;
            const top = Math.max(yMin, max ?? 0, ref) * 1.15;
            return [0, top];
          },
        },
      },
      axes: [
        {
          stroke: muted,
          font,
          grid: { stroke: line, width: 1 },
          ticks: { stroke: line, width: 1, size: 4 },
          values: (_u, vals) => {
            const step = vals.length > 1 ? vals[1]! - vals[0]! : 60;
            return vals.map((v) => (step < 60 ? timeFmt : axisTimeFmt).format(v * 1000));
          },
        },
        {
          stroke: muted,
          font,
          size: 72,
          grid: { stroke: line, width: 1 },
          ticks: { show: false },
          values: (_u, vals) => vals.map((v) => fmtRef.current(v)),
        },
      ],
      series: [
        { value: (_u, v) => (v == null ? "—" : timeFmt.format(v * 1000)) },
        ...series.map((s) => {
          const color = cssVar(s.colorVar);
          return {
            label: s.label,
            stroke: color,
            width: 2,
            dash: s.dashed ? [6, 4] : undefined,
            fill: single ? withAlpha(color, 0.14) : undefined,
            points: { show: false },
            spanGaps: false,
            value: (_u: uPlot, v: number | null) => (v == null ? "—" : fmtRef.current(v)),
          };
        }),
      ],
      hooks: {
        draw: [
          (u) => {
            const ref = refRef.current;
            if (!ref) return;
            const y = u.valToPos(ref.value, "y", true);
            const ctx = u.ctx;
            ctx.save();
            ctx.strokeStyle = cssVar("--text-subtle");
            ctx.setLineDash([4, 4]);
            ctx.lineWidth = 1;
            ctx.beginPath();
            ctx.moveTo(u.bbox.left, y);
            ctx.lineTo(u.bbox.left + u.bbox.width, y);
            ctx.stroke();
            ctx.setLineDash([]);
            ctx.fillStyle = cssVar("--text-subtle");
            ctx.font = font.replace("11px", `${11 * devicePixelRatio}px`);
            ctx.fillText(ref.label, u.bbox.left + 6 * devicePixelRatio, y - 6 * devicePixelRatio);
            ctx.restore();
          },
        ],
      },
    };

    opts.hooks!.setCursor = [
      (u) => {
        if (u.cursor.idx == null) {
          const n = u.data[0]?.length ?? 0;
          if (n > 0) u.setLegend({ idx: n - 1 });
        }
      },
    ];
    const u = new uPlot(opts, [times.map((t) => t / 1000), ...series.map((s) => s.values)] as uPlot.AlignedData, el);
    plot.current = u;
    if (times.length > 0) u.setLegend({ idx: times.length - 1 });
    const ro = new ResizeObserver(() => u.setSize({ width: el.clientWidth, height }));
    ro.observe(el);
    return () => {
      ro.disconnect();
      u.destroy();
      plot.current = null;
    };
  }, [shape, height, themeVersion, yMin]);

  useEffect(() => {
    const u = plot.current;
    if (!u) return;
    u.setData([times.map((t) => t / 1000), ...series.map((s) => s.values)] as uPlot.AlignedData);
    if (u.cursor.idx == null && times.length > 0) u.setLegend({ idx: times.length - 1 });
  }, [times, series]);

  return <div ref={host} className="timechart" role="img" aria-label={label} />;
}
