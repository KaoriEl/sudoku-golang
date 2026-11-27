import type { ReactNode } from "react";

interface FrameProps {
  children: ReactNode;
  className?: string;
  flush?: boolean;
  as?: "section" | "div";
  label?: string;
}

export function Frame({ children, className = "", flush = false, as: Tag = "section", label }: FrameProps) {
  return (
    <Tag className={`frame ${flush ? "frame-flush" : ""} ${className}`} aria-label={label}>
      <span className="bracket bracket-tr" aria-hidden />
      <span className="bracket bracket-bl" aria-hidden />
      <div className="frame-body">{children}</div>
    </Tag>
  );
}

interface SectionBarProps {
  title: string;
  jp?: string;
  count?: string;
  level?: 1 | 2 | 3;
  children?: ReactNode;
}

export function SectionBar({ title, jp, count, level = 3, children }: SectionBarProps) {
  const Heading = level === 1 ? "h1" : level === 2 ? "h2" : "h3";
  return (
    <div className={`section-bar section-bar-${level}`}>
      <span className="section-mark" aria-hidden />
      <Heading className="section-title">{title}</Heading>
      {jp && (
        <span className="jp" aria-hidden>
          {jp}
        </span>
      )}
      <span className="section-rule" aria-hidden />
      {count && <span className="section-count mono">{count}</span>}
      {children && <div className="section-actions">{children}</div>}
    </div>
  );
}
