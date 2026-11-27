import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from "react";
import { X } from "lucide-react";
import { ru } from "../../i18n/ru";

type Tone = "success" | "danger" | "progress";

interface Toast {
  id: number;
  tone: Tone;
  message: string;
  detail?: string;
  action?: { label: string; onClick: () => void };
}

type Push = (t: Omit<Toast, "id">) => void;

const ToastContext = createContext<Push>(() => undefined);

export function useToast(): Push {
  return useContext(ToastContext);
}

const titles: Record<Tone, string> = {
  success: ru.toast.done,
  danger: ru.toast.fail,
  progress: ru.toast.info,
};

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const next = useRef(1);

  const dismiss = useCallback((id: number) => setToasts((all) => all.filter((t) => t.id !== id)), []);

  const push = useCallback<Push>(
    (t) => {
      const id = next.current++;
      setToasts((all) => [...all.slice(-2), { ...t, id }]);
      if (t.tone !== "danger") window.setTimeout(() => dismiss(id), 5000);
    },
    [dismiss],
  );

  const value = useMemo(() => push, [push]);

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div className="toasts" role="region" aria-live="polite" aria-label={ru.toast.region}>
        {toasts.map((t) => (
          <div key={t.id} className={`toast toast-${t.tone}`} role={t.tone === "danger" ? "alert" : "status"}>
            <div className="toast-text">
              <div className="toast-title">{titles[t.tone]}</div>
              <div className="toast-message">{t.message}</div>
              {t.detail && <div className="toast-detail mono">{t.detail}</div>}
              {t.action && (
                <button type="button" className="link-btn" onClick={t.action.onClick}>
                  {t.action.label}
                </button>
              )}
            </div>
            <button type="button" className="toast-close" onClick={() => dismiss(t.id)} aria-label={ru.toast.close}>
              <X size={14} strokeWidth={2} strokeLinecap="square" />
            </button>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}
