import * as Dialog from "@radix-ui/react-dialog";
import { Play, Square } from "lucide-react";
import { useCallback, useRef, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError, api } from "../../api/client";
import type { PortConflict, PreflightRequest } from "../../api/types";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { Button } from "../../components/ui/Button";
import { useToast } from "../../components/ui/Toasts";
import { rf } from "../../i18n/ru.features";

const t = rf.preflight;
const iconProps = { size: 14, strokeWidth: 2, strokeLinecap: "square" as const };

export type PreflightGuard = (req: PreflightRequest, proceed: () => void) => void;

export function usePreflight(): [PreflightGuard, ReactNode] {
  const qc = useQueryClient();
  const toast = useToast();
  const [conflicts, setConflicts] = useState<PortConflict[] | null>(null);
  const [stopping, setStopping] = useState<string | null>(null);
  const proceedRef = useRef<() => void>(() => undefined);

  const guard = useCallback<PreflightGuard>((req, proceed) => {
    api
      .preflight(req)
      .then((res) => {
        if (res.conflicts.length === 0) {
          proceed();
          return;
        }
        proceedRef.current = proceed;
        setConflicts(res.conflicts);
      })
      .catch(() => proceed());
  }, []);

  const stopOwner = (c: PortConflict) => {
    if (!c.containerId) return;
    setStopping(c.containerId);
    api
      .container(c.containerId, "stop")
      .then(() => {
        toast({ tone: "success", message: t.stopped(c.owner) });
        setConflicts((list) => {
          const next = (list ?? []).filter((x) => x.containerId !== c.containerId);
          return next.length > 0 ? next : null;
        });
        void qc.invalidateQueries({ queryKey: ["state"] });
      })
      .catch((e: unknown) => toast({ tone: "danger", message: t.stop(c.owner), detail: e instanceof ApiError ? e.message : String(e) }))
      .finally(() => setStopping(null));
  };

  const open = conflicts !== null;
  const dialog = (
    <Dialog.Root open={open} onOpenChange={(o) => !o && setConflicts(null)}>
      <Dialog.Portal>
        <Dialog.Overlay className="scrim" />
        <Dialog.Content className="dialog dialog-wide">
          <div className="hazard-band hazard-warning" aria-hidden />
          <span className="bracket bracket-tr" aria-hidden />
          <span className="bracket bracket-bl" aria-hidden />
          <div className="dialog-inner">
            <Dialog.Title className="dialog-title">
              <StatusGlyph kind="triangle" size={14} />
              {t.title}
            </Dialog.Title>
            <Dialog.Description asChild>
              <div className="dialog-body">
                <p>{t.body(conflicts?.length ?? 0)}</p>
                <ul className="conflict-list">
                  {(conflicts ?? []).map((c) => (
                    <li key={`${c.file}-${c.service}-${c.port}-${c.proto}`} className="conflict">
                      <span className="mono">
                        {c.port}/{c.proto}
                      </span>
                      <span>
                        <b>{c.service}</b> · <span className="meta">{c.owner}</span>
                      </span>
                      {c.kind === "container" && c.containerId ? (
                        <Button size="sm" variant="danger-outline" icon={<Square {...iconProps} />} loading={stopping === c.containerId} onClick={() => stopOwner(c)}>
                          {t.stop(c.owner.replace(/^контейнер /, ""))}
                        </Button>
                      ) : (
                        <span className="meta micro">{t.process}</span>
                      )}
                    </li>
                  ))}
                </ul>
              </div>
            </Dialog.Description>
            <div className="dialog-actions">
              <Dialog.Close asChild>
                <Button variant="outline" autoFocus>
                  {t.cancel}
                </Button>
              </Dialog.Close>
              <Button
                variant="primary"
                icon={<Play {...iconProps} />}
                onClick={() => {
                  setConflicts(null);
                  proceedRef.current();
                }}
              >
                {t.proceed}
              </Button>
            </div>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
  return [guard, dialog];
}
