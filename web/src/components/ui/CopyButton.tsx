import { useEffect, useState } from "react";
import { Check, Copy } from "lucide-react";
import { ru } from "../../i18n/ru";

export function CopyText({ value, children, className = "" }: { value: string; children?: React.ReactNode; className?: string }) {
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const t = window.setTimeout(() => setCopied(false), 1400);
    return () => window.clearTimeout(t);
  }, [copied]);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };

  return (
    <button type="button" className={`copy ${copied ? "is-copied" : ""} ${className}`} onClick={copy} title={copied ? ru.copied : `${ru.copy}: ${value}`}>
      <span className="copy-value">{children ?? value}</span>
      <span className="copy-icon" aria-hidden>
        {copied ? <Check size={12} strokeWidth={2} strokeLinecap="square" /> : <Copy size={12} strokeWidth={2} strokeLinecap="square" />}
      </span>
      <span className="visually-hidden" aria-live="polite">
        {copied ? ru.copied : ""}
      </span>
    </button>
  );
}
