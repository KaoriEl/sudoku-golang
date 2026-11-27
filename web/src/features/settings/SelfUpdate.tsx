import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, ExternalLink, RefreshCw, RotateCw } from "lucide-react";
import { ApiError, api } from "../../api/client";
import type { SelfUpdateView } from "../../api/types";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { Button } from "../../components/ui/Button";
import { ConfirmDialog } from "../../components/ui/ConfirmDialog";
import { CopyText } from "../../components/ui/CopyButton";
import { useToast } from "../../components/ui/Toasts";
import { rf } from "../../i18n/ru.features";
import { absolute } from "../../lib/format";
import { useRestartAdmin } from "./useRestartAdmin";

const t = rf.prefs;
const iconProps = { size: 14, strokeWidth: 2, strokeLinecap: "square" as const };
const KEY = ["selfupdate"];

function errorText(e: unknown): string {
  return e instanceof ApiError ? e.message : String(e);
}

export function SelfUpdate({ canRestart }: { canRestart: boolean }) {
  const qc = useQueryClient();
  const toast = useToast();
  const q = useQuery({ queryKey: KEY, queryFn: api.selfUpdate });
  const restart = useRestartAdmin();
  const [confirmForce, setConfirmForce] = useState(false);
  const autoRan = useRef(false);
  const silent = useRef(false);
  const put = (v: SelfUpdateView) => qc.setQueryData(KEY, v);

  const check = useMutation({
    mutationFn: api.checkSelfUpdate,
    onSuccess: (v) => {
      put(v);
      if (v.checkError && !silent.current) toast({ tone: "danger", message: t.checkFailed, detail: v.checkError });
    },
    onError: (e) => toast({ tone: "danger", message: t.checkFailed, detail: errorText(e) }),
  });

  const apply = useMutation({
    mutationFn: (force: boolean) => api.applySelfUpdate(force),
    onSuccess: (v) => {
      put(v);
      if (v.installed) toast({ tone: "success", message: t.installedToast(v.installed) });
    },
    onError: (e) => toast({ tone: "danger", message: t.applyFailed, detail: errorText(e) }),
  });

  const v = q.data;

  useEffect(() => {
    if (v && !v.checkedAt && !autoRan.current) {
      autoRan.current = true;
      silent.current = true;
      check.mutate(undefined, { onSettled: () => (silent.current = false) });
    }
  }, [v]);

  if (!v) return <div className="skeleton-row" />;

  const latest = v.latest;
  const busy = check.isPending || apply.isPending || restart.busy;
  const pending = Boolean(v.installed);

  return (
    <div className="selfupdate">
      {pending && (
        <div className="notice notice-apply" role="status">
          <div className="hazard-band" aria-hidden />
          <StatusGlyph kind="triangle" size={12} />
          <span className="notice-text">{t.installed(v.installed ?? "")}</span>
          <Button
            variant="primary"
            icon={<RotateCw {...iconProps} />}
            loading={restart.busy}
            loadingLabel={t.restarting}
            disabled={!canRestart}
            disabledReason={t.restartUnavailable}
            onClick={restart.restart}
          >
            {t.restart}
          </Button>
        </div>
      )}

      {latest && v.newer && !pending && (
        <div className="notice notice-warning selfupdate-available" role="status">
          <StatusGlyph kind="triangle" size={12} />
          <div className="notice-text">
            <div className="notice-title">{t.available(latest.tag)}</div>
            <p className="meta micro">{t.published(absolute(latest.published))}</p>
            {latest.notes && (
              <details className="selfupdate-notes">
                <summary>{t.notes}</summary>
                <pre className="mono micro">{latest.notes}</pre>
              </details>
            )}
            <p className="meta micro">{t.applyHint}</p>
          </div>
          <div className="selfupdate-actions">
            <Button
              variant="primary"
              icon={<Download {...iconProps} />}
              loading={apply.isPending}
              loadingLabel={t.applying}
              disabled={!v.canApply || busy}
              disabledReason={v.reason}
              onClick={() => apply.mutate(false)}
            >
              {t.apply(latest.tag)}
            </Button>
            {latest.url && (
              <a className="btn btn-ghost btn-sm" href={latest.url} target="_blank" rel="noreferrer">
                <ExternalLink {...iconProps} />
                <span className="btn-label">{t.releasePage}</span>
              </a>
            )}
          </div>
        </div>
      )}

      {latest && !v.newer && !pending && (
        <p className="tone-success inline-status">
          <StatusGlyph kind="square" size={10} /> {v.isRelease ? t.upToDate : t.dev}
          {!v.isRelease && v.canApply && (
            <Button size="sm" variant="ghost" icon={<Download {...iconProps} />} disabled={busy} onClick={() => setConfirmForce(true)}>
              {t.forceApply(latest.tag)}
            </Button>
          )}
        </p>
      )}

      {v.checkError && (
        <p className="meta micro">
          <StatusGlyph kind="fault" size={10} /> {t.checkFailed}: <span className="mono">{v.checkError}</span>
        </p>
      )}
      {v.reason && latest && (
        <p className="meta micro">
          {t.cantApply} {v.reason}. {t.terminal} <CopyText value="sudoku update" className="mono" />
        </p>
      )}

      <div className="restart-admin">
        <Button
          variant="outline"
          icon={<RefreshCw {...iconProps} />}
          loading={check.isPending}
          loadingLabel={t.checking}
          disabled={busy}
          onClick={() => check.mutate()}
        >
          {t.check}
        </Button>
        {!pending && (
          <Button
            variant="ghost"
            icon={<RotateCw {...iconProps} />}
            loading={restart.busy}
            loadingLabel={t.restarting}
            disabled={!canRestart || busy}
            disabledReason={t.restartUnavailable}
            onClick={restart.restart}
          >
            {t.restart}
          </Button>
        )}
        <span className="meta micro">
          {t.current} <span className="mono">{v.current}</span>
          {v.checkedAt && <> · {t.checkedAt(absolute(v.checkedAt))}</>}
        </span>
      </div>

      <ConfirmDialog
        open={confirmForce}
        onOpenChange={setConfirmForce}
        title={t.forceTitle}
        body={<p>{t.forceBody(latest?.tag ?? "", v.exe)}</p>}
        confirmLabel={t.forceApply(latest?.tag ?? "")}
        confirmIcon={<Download {...iconProps} />}
        onConfirm={() => apply.mutate(true)}
      />
    </div>
  );
}
