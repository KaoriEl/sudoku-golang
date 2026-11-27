import type { Status } from "../../api/types";

type GlyphKind = "square" | "outline" | "triangle" | "fault" | "ring" | "dashed";

const kindByStatus: Record<Status, GlyphKind> = {
  running: "square",
  starting: "triangle",
  unhealthy: "triangle",
  restarting: "triangle",
  paused: "outline",
  stopped: "outline",
  error: "fault",
};

interface Props {
  status?: Status;
  kind?: GlyphKind;
  size?: number;
  label?: string;
}

export function StatusGlyph({ status, kind, size = 10, label }: Props) {
  const k = kind ?? (status ? kindByStatus[status] : "outline");
  const a11y = label ? { role: "img", "aria-label": label } : { "aria-hidden": true };

  return (
    <svg className={`glyph glyph-${k}`} width={size} height={size} viewBox="0 0 12 12" {...a11y}>
      {k === "square" && <rect x="1.5" y="1.5" width="9" height="9" fill="currentColor" />}
      {k === "outline" && <rect x="2.25" y="2.25" width="7.5" height="7.5" fill="none" stroke="currentColor" strokeWidth="1.5" />}
      {k === "dashed" && (
        <rect x="2.25" y="2.25" width="7.5" height="7.5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeDasharray="2 1.5" />
      )}
      {k === "triangle" && <polygon points="6,1.2 11,10.8 1,10.8" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="miter" />}
      {k === "fault" && (
        <>
          <polygon points="6,0.5 11.8,11.2 0.2,11.2" fill="currentColor" />
          <rect x="5.2" y="4" width="1.6" height="4" fill="var(--bg)" />
          <rect x="5.2" y="8.8" width="1.6" height="1.4" fill="var(--bg)" />
        </>
      )}
      {k === "ring" &&
        Array.from({ length: 8 }, (_, i) => {
          const angle = (i * Math.PI) / 4;
          const x1 = 6 + Math.cos(angle) * 3;
          const y1 = 6 + Math.sin(angle) * 3;
          const x2 = 6 + Math.cos(angle) * 5.4;
          const y2 = 6 + Math.sin(angle) * 5.4;
          return (
            <line key={i} x1={x1} y1={y1} x2={x2} y2={y2} stroke="currentColor" strokeWidth="1.5" opacity={0.3 + (i / 8) * 0.7} />
          );
        })}
    </svg>
  );
}
