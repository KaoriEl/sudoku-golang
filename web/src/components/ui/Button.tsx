import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from "react";
import { StatusGlyph } from "../hud/StatusGlyph";

type Variant = "primary" | "outline" | "ghost" | "danger" | "danger-outline";
type Size = "sm" | "md" | "lg";

interface Props extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  size?: Size;
  icon?: ReactNode;
  loading?: boolean;
  loadingLabel?: string;
  disabledReason?: string;
  iconOnly?: boolean;
}

export const Button = forwardRef<HTMLButtonElement, Props>(function Button(
  { variant = "outline", size = "md", icon, loading, loadingLabel, disabledReason, iconOnly, children, disabled, title, className = "", ...rest },
  ref,
) {
  const isDisabled = disabled || loading;
  const tip = isDisabled && disabledReason ? disabledReason : title;

  return (
    <button
      ref={ref}
      type="button"
      className={`btn btn-${variant} btn-${size} ${iconOnly ? "btn-icon" : ""} ${loading ? "is-loading" : ""} ${className}`}
      disabled={isDisabled}
      aria-disabled={isDisabled}
      aria-busy={loading || undefined}
      title={tip}
      {...rest}
    >
      {loading ? <span className="spin"><StatusGlyph kind="ring" size={14} /></span> : icon}
      {!iconOnly && <span className="btn-label">{loading && loadingLabel ? loadingLabel : children}</span>}
      {iconOnly && <span className="visually-hidden">{children}</span>}
    </button>
  );
});
