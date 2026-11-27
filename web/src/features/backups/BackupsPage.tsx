import * as Dialog from "@radix-ui/react-dialog";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArchiveRestore, DatabaseBackup, Download, Trash2 } from "lucide-react";
import { useState } from "react";
import { ApiError, api, backupDownloadUrl } from "../../api/client";
import type { BackupItem, Run } from "../../api/types";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { Button } from "../../components/ui/Button";
import { ConfirmDialog } from "../../components/ui/ConfirmDialog";
import { Frame, SectionBar } from "../../components/ui/Frame";
import { useToast } from "../../components/ui/Toasts";
import { ru } from "../../i18n/ru";
import { rf } from "../../i18n/ru.features";
import { absolute, ago } from "../../lib/format";
import { useNow } from "../../lib/hooks";
import { bytes } from "../../lib/metrics";

const t = rf.backups;
const iconProps = { size: 14, strokeWidth: 2, strokeLinecap: "square" as const };

function errorText(e: unknown): string {
  return e instanceof ApiError ? e.message : String(e);
}

function RestoreDialog({ item, onClose, onConfirm }: { item: BackupItem | null; onClose: () => void; onConfirm: (b: BackupItem) => void }) {
  const [typed, setTyped] = useState("");
  const ok = item !== null && typed.trim() === item.container;
  return (
    <Dialog.Root
      open={item !== null}
      onOpenChange={(o) => {
        if (!o) {
          setTyped("");
          onClose();
        }
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="scrim" />
        <Dialog.Content className="dialog dialog-danger">
          <div className="hazard-band" aria-hidden />
          <span className="bracket bracket-tr" aria-hidden />
          <span className="bracket bracket-bl" aria-hidden />
          {item && (
            <form
              className="dialog-inner"
              onSubmit={(e) => {
                e.preventDefault();
                if (!ok) return;
                setTyped("");
                onConfirm(item);
              }}
            >
              <Dialog.Title className="dialog-title">
                <StatusGlyph kind="fault" size={14} />
                {t.restoreTitle}
              </Dialog.Title>
              <Dialog.Description asChild>
                <div className="dialog-body">
                  <p>{t.restoreBody(item.container, absolute(item.createdAt))}</p>
                  <label className="field">
                    <span className="field-label">{t.restoreType(item.container)}</span>
                    <span className="input">
                      <input className="mono" value={typed} autoComplete="off" onChange={(e) => setTyped(e.target.value)} />
                    </span>
                  </label>
                </div>
              </Dialog.Description>
              <div className="dialog-actions">
                <Dialog.Close asChild>
                  <Button variant="outline" autoFocus>
                    {ru.confirm.cancel}
                  </Button>
                </Dialog.Close>
                <Button type="submit" variant="danger" icon={<ArchiveRestore {...iconProps} />} disabled={!ok}>
                  {t.restore}
                </Button>
              </div>
            </form>
          )}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

export function BackupsPage({ bulkBusy, onRunStarted }: { bulkBusy: boolean; onRunStarted: (r: Run) => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const now = useNow(30_000);
  const q = useQuery({ queryKey: ["backups"], queryFn: api.backups, refetchInterval: bulkBusy ? 3000 : 15_000 });
  const [restoring, setRestoring] = useState<BackupItem | null>(null);
  const [deleting, setDeleting] = useState<BackupItem | null>(null);

  const create = useMutation({
    mutationFn: api.createBackups,
    onSuccess: onRunStarted,
    onError: (e) => toast({ tone: "danger", message: t.backup, detail: errorText(e) }),
  });
  const restore = useMutation({
    mutationFn: (b: BackupItem) => api.restoreBackup(b.id),
    onSuccess: onRunStarted,
    onError: (e) => toast({ tone: "danger", message: t.restore, detail: errorText(e) }),
  });
  const remove = useMutation({
    mutationFn: (b: BackupItem) => api.deleteBackup(b.id),
    onSuccess: () => {
      toast({ tone: "success", message: t.deleted });
      void qc.invalidateQueries({ queryKey: ["backups"] });
    },
    onError: (e) => toast({ tone: "danger", message: t.delete, detail: errorText(e) }),
  });

  const v = q.data;
  const running = (v?.targets ?? []).filter((x) => x.running);
  const busy = bulkBusy || create.isPending;

  return (
    <main className="page" id="main">
      <header className="page-head">
        <SectionBar title={t.title} jp={t.titleJp} level={1} count={`${v?.targets.length ?? 0} DB`}>
          <Button
            variant="primary"
            icon={<DatabaseBackup {...iconProps} />}
            disabled={busy || running.length === 0}
            disabledReason={bulkBusy ? ru.bulk.busy : undefined}
            loading={create.isPending}
            onClick={() => create.mutate(running.map((x) => x.id))}
          >
            {t.backupAll}
          </Button>
        </SectionBar>
        {v && (
          <p className="page-summary">
            {t.summary(v.targets.length, v.dir)} · {t.keep(v.keep)}
          </p>
        )}
      </header>

      {q.isError && (
        <div className="empty">
          <StatusGlyph kind="fault" size={32} />
          <p className="mono">{errorText(q.error)}</p>
          <Button variant="outline" onClick={() => void q.refetch()}>
            {ru.error.retry}
          </Button>
        </div>
      )}

      <Frame flush label={t.targets}>
        <div className="frame-pad">
          <SectionBar title={t.targets} />
        </div>
        {!v ? (
          <div className="skeleton-rows">
            <div className="skeleton-row" />
            <div className="skeleton-row" />
          </div>
        ) : v.targets.length === 0 ? (
          <p className="meta frame-pad">{t.noTargets}</p>
        ) : (
          <div className="table-scroll">
            <table className="table">
              <thead>
                <tr>
                  <th>{t.columns.name}</th>
                  <th>{t.columns.kind}</th>
                  <th>{t.columns.state}</th>
                  <th>{t.columns.last}</th>
                  <th className="num">{t.columns.count}</th>
                  <th className="th-actions" />
                </tr>
              </thead>
              <tbody>
                {v.targets.map((x) => (
                  <tr key={x.id}>
                    <td>
                      <div className="mono">{x.name}</div>
                      <div className="meta micro mono">{x.image}</div>
                    </td>
                    <td>{t.kinds[x.kind] ?? x.kind}</td>
                    <td>
                      <span className={`badge badge-${x.running ? "success" : "neutral"}`}>
                        <StatusGlyph kind={x.running ? "square" : "outline"} size={10} />
                        {x.running ? ru.status.running : ru.status.stopped}
                      </span>
                    </td>
                    <td title={x.last ? absolute(x.last) : undefined}>{x.last ? ago(x.last, now) : t.never}</td>
                    <td className="num mono">{x.count}</td>
                    <td className="row-actions">
                      <Button
                        size="sm"
                        variant="outline"
                        icon={<DatabaseBackup {...iconProps} />}
                        disabled={busy || !x.running}
                        disabledReason={!x.running ? t.notRunning : bulkBusy ? ru.bulk.busy : undefined}
                        onClick={() => create.mutate([x.id])}
                      >
                        {t.backup}
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Frame>

      <Frame flush label={t.list}>
        <div className="frame-pad">
          <SectionBar title={t.list} count={String(v?.backups.length ?? 0)} />
        </div>
        {v && v.backups.length === 0 ? (
          <p className="meta frame-pad">{t.noBackups}</p>
        ) : (
          <div className="table-scroll">
            <table className="table">
              <thead>
                <tr>
                  <th>{t.columns.name}</th>
                  <th>{t.columns.kind}</th>
                  <th>{t.columns.created}</th>
                  <th className="num">{t.columns.size}</th>
                  <th className="th-actions" />
                </tr>
              </thead>
              <tbody>
                {(v?.backups ?? []).map((b) => (
                  <tr key={b.id}>
                    <td>
                      <div className="mono">{b.container}</div>
                      <div className="meta micro mono" title={b.file}>
                        {b.id}
                      </div>
                    </td>
                    <td>{t.kinds[b.kind] ?? b.kind}</td>
                    <td title={absolute(b.createdAt)}>{ago(b.createdAt, now)}</td>
                    <td className="num mono">{bytes(b.size)}</td>
                    <td className="row-actions">
                      <a className="btn btn-ghost btn-sm" href={backupDownloadUrl(b.id)} download>
                        <Download {...iconProps} aria-hidden />
                        <span className="btn-label">{t.download}</span>
                      </a>
                      <Button size="sm" variant="danger-outline" icon={<ArchiveRestore {...iconProps} />} disabled={busy} disabledReason={bulkBusy ? ru.bulk.busy : undefined} onClick={() => setRestoring(b)}>
                        {t.restore}
                      </Button>
                      <Button size="sm" variant="ghost" iconOnly icon={<Trash2 {...iconProps} />} title={t.delete} onClick={() => setDeleting(b)}>
                        {t.delete}
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Frame>

      <RestoreDialog
        item={restoring}
        onClose={() => setRestoring(null)}
        onConfirm={(b) => {
          setRestoring(null);
          restore.mutate(b);
        }}
      />
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(o) => !o && setDeleting(null)}
        danger
        title={t.delete}
        body={<p>{deleting ? t.deleteBody(deleting.file) : ""}</p>}
        confirmLabel={t.delete}
        confirmIcon={<Trash2 {...iconProps} />}
        onConfirm={() => deleting && remove.mutate(deleting)}
      />
    </main>
  );
}
