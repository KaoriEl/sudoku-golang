interface Props {
  values: number[];
  width?: number;
  height?: number;
  max?: number;
  fit?: boolean;
  label: string;
}

export function Sparkline({ values, width = 96, height = 24, max, fit, label }: Props) {
  if (values.length < 2) {
    return <svg className="sparkline" width={width} height={height} role="img" aria-label={label} />;
  }
  const hi = Math.max(max ?? 0, ...values, 1e-9);
  const lo0 = fit ? Math.min(...values) : 0;
  const pad = fit ? Math.max((hi - lo0) * 0.25, hi * 0.01) : 0;
  const lo = Math.max(0, lo0 - pad);
  const top = hi + pad;
  const span = Math.max(top - lo, 1e-9);
  const step = width / (values.length - 1);
  const pts = values.map((v, i) => `${(i * step).toFixed(1)},${(height - 1 - ((v - lo) / span) * (height - 2)).toFixed(1)}`);
  const area = `0,${height} ${pts.join(" ")} ${width},${height}`;
  return (
    <svg className="sparkline" width={width} height={height} viewBox={`0 0 ${width} ${height}`} role="img" aria-label={label}>
      <polygon points={area} className="sparkline-fill" />
      <polyline points={pts.join(" ")} className="sparkline-line" />
    </svg>
  );
}

export function Meter({ value, max, tone, label }: { value: number; max: number; tone?: "warning" | "danger"; label: string }) {
  const ratio = max > 0 ? Math.min(1, Math.max(0, value / max)) : 0;
  return (
    <span className={`meter ${tone ? `meter-${tone}` : ""}`} role="meter" aria-valuemin={0} aria-valuemax={max} aria-valuenow={value} aria-label={label}>
      <span className="meter-fill" style={{ width: `${(ratio * 100).toFixed(1)}%` }} />
    </span>
  );
}
