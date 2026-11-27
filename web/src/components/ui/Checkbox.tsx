import type { ReactNode } from "react";

interface Props {
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
  children: ReactNode;
  className?: string;
  title?: string;
}

export function Checkbox({ checked, onChange, disabled, children, className = "", title }: Props) {
  return (
    <label className={`check ${disabled ? "is-disabled" : ""} ${className}`} title={title}>
      <input type="checkbox" checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} />
      <span className="check-box" aria-hidden />
      <span>{children}</span>
    </label>
  );
}

export function Switch({ checked, onChange, disabled, children, title }: Omit<Props, "className">) {
  return (
    <label className={`switch ${disabled ? "is-disabled" : ""}`} title={title}>
      <input type="checkbox" role="switch" checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} />
      <span className="switch-track" aria-hidden>
        <span className="switch-thumb" />
        <span className="switch-text mono">{checked ? "ON" : "OFF"}</span>
      </span>
      <span>{children}</span>
    </label>
  );
}
