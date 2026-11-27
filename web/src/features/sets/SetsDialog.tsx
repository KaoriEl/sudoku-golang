import * as Dialog from "@radix-ui/react-dialog";
import { Play, RotateCw, Save, Square, Trash2, X } from "lucide-react";
import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, api } from "../../api/client";
import type { BulkAction, Run, SetsView } from "../../api/types";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { Button } from "../../components/ui/Button";
import { SectionBar } from "../../components/ui/Frame";
import { useToast } from "../../components/ui/Toasts";
import { rf } from "../../i18n/ru.features";
import type { PreflightGuard } from "../preflight/Preflight";

const t = rf.sets;
const iconProps = { size: 14, strokeWidth: 2, strokeLinecap: "square" as const };

function errorText(e: unknown): string {
  return e instanceof ApiError ? e.message : String(e);
}

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  draftTargets: string[];
  bulkBusy: boolean;
  guard: PreflightGuard;
  onRunStarted: (r: Run) => void;
}

export function SetsDialog({ open, onOpenChange, draftTargets, bulkBusy, guard, onRunStarted }: Props) {
  const qc = useQueryClient();
  const toast = useToast();
  const q = useQuery({ queryKey: ["sets"], queryFn: api.sets, enabled: open });
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [targets, setTargets] = useState("");
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setTargets(draftTargets.join(" "));
      setName("");
      setDesc("");
      setConfirmDelete(null);
    }
  }, [open, draftTargets]);

  const setView = (v: SetsView) => qc.setQueryData(["sets"], v);
  const save = useMutation({
    mutationFn: () => api.saveSet(name.trim(), desc.trim(), targets.split(/\s+/).filter(Boolean)),
    onSuccess: (v) => {
      setView(v);
      toast({ tone: "success", message: t.saved(name.trim()) });
      setName("");
      setDesc("");
    },
    onError: (e) => toast({ tone: "danger", message: t.save, detail: errorText(e) }),
  });
  const remove = useMutation({
    mutationFn: (n: string) => api.deleteSet(n),
    onSuccess: (v, n) => {
      setView(v);
      setConfirmDelete(null);
      toast({ tone: "success", message: t.deleted(n) });
    },
    onError: (e) => toast({ tone: "danger", message: t.delete, detail: errorText(e) }),
  });
  const run = useMutation({
    mutationFn: ({ set, action }: { set: string; action: BulkAction }) => api.runSet(set, action),
    onSuccess: (r) => {
      onOpenChange(false);
      onRunStarted(r);
    },
    onError: (e) => toast({ tone: "danger", message: t.start, detail: errorText(e) }),
  });
  const start = (set: string, action: BulkAction) => {
    if (action === "stop") run.mutate({ set, action });
    else guard({ set }, () => run.mutate({ set, action }));
  };

  const sets = q.data?.sets ?? [];
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange} modal={false}>
      <Dialog.Portal>
        <Dialog.Content className="drawer" onInteractOutside={(e) => e.preventDefault()} aria-describedby={undefined}>
          <span className="bracket bracket-tl" aria-hidden />
          <span className="bracket bracket-bl" aria-hidden />
          <div className="drawer-head">
            <SectionBar title={t.title} jp={t.titleJp} level={2}>
              <Dialog.Close className="btn btn-ghost btn-sm btn-icon" aria-label={rf.preflight.cancel}>
                <X size={16} strokeWidth={2} strokeLinecap="square" />
              </Dialog.Close>
            </SectionBar>
            <Dialog.Title className="visually-hidden">{t.title}</Dialog.Title>
          </div>
          <div className="drawer-body">
            {q.isLoading && <div className="skeleton-row" />}
            {!q.isLoading && sets.length === 0 && <p className="meta">{t.empty}</p>}
            <ul className="set-list">
              {sets.map((s) => (
                <li key={s.name} className={`set-item ${s.error ? "is-broken" : ""}`}>
                  <div className="set-head">
                    <span className="mono set-name">{s.name}</span>
                    <span className="meta micro">{s.error ? t.broken : t.services(s.resolved.length)}</span>
                  </div>
                  {s.description && <p className="meta">{s.description}</p>}
                  <p className="mono micro set-targets">{s.targets.join(" ")}</p>
                  {s.error && (
                    <p className="tone-danger meta inline-status">
                      <StatusGlyph kind="fault" size={10} /> {s.error}
                    </p>
                  )}
                  <div className="set-actions">
                    <Button size="sm" variant="primary" icon={<Play {...iconProps} />} disabled={bulkBusy || !!s.error || run.isPending} onClick={() => start(s.name, "start")}>
                      {t.start}
                    </Button>
                    <Button size="sm" variant="outline" icon={<RotateCw {...iconProps} />} disabled={bulkBusy || !!s.error || run.isPending} onClick={() => start(s.name, "restart")}>
                      {t.restart}
                    </Button>
                    <Button size="sm" variant="danger-outline" icon={<Square {...iconProps} />} disabled={bulkBusy || !!s.error || run.isPending} onClick={() => start(s.name, "stop")}>
                      {t.stop}
                    </Button>
                    {confirmDelete === s.name ? (
                      <Button size="sm" variant="danger" icon={<Trash2 {...iconProps} />} loading={remove.isPending} onClick={() => remove.mutate(s.name)} title={t.deleteConfirm(s.name)}>
                        {t.delete}
                      </Button>
                    ) : (
                      <Button size="sm" variant="ghost" iconOnly icon={<Trash2 {...iconProps} />} title={t.delete} onClick={() => setConfirmDelete(s.name)}>
                        {t.delete}
                      </Button>
                    )}
                    <code className="mono micro meta">{t.cli(s.name)}</code>
                  </div>
                </li>
              ))}
            </ul>

            <form
              className="form set-form"
              onSubmit={(e) => {
                e.preventDefault();
                save.mutate();
              }}
            >
              <SectionBar title={t.saveSelection} />
              <label className="field">
                <span className="field-label">{t.name}</span>
                <span className="input">
                  <input className="mono" value={name} maxLength={64} pattern="[A-Za-z0-9][A-Za-z0-9_.\-]*" required onChange={(e) => setName(e.target.value)} />
                </span>
                <span className="field-help">{t.nameHelp}</span>
              </label>
              <label className="field">
                <span className="field-label">{t.description}</span>
                <span className="input">
                  <input value={desc} maxLength={200} onChange={(e) => setDesc(e.target.value)} />
                </span>
              </label>
              <label className="field">
                <span className="field-label">{t.targets}</span>
                <span className="input">
                  <input className="mono" value={targets} required onChange={(e) => setTargets(e.target.value)} />
                </span>
                <span className="field-help">{t.targetsHelp}</span>
              </label>
              <div className="form-actions">
                <Button type="submit" variant="outline" icon={<Save {...iconProps} />} loading={save.isPending} disabled={!name.trim() || !targets.trim()}>
                  {t.save}
                </Button>
              </div>
            </form>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
