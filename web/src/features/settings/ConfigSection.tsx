import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { RotateCcw, Save } from "lucide-react";
import { ApiError, api } from "../../api/client";
import type { ConfigField, ConfigSaved } from "../../api/types";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { Button } from "../../components/ui/Button";
import { ConfirmDialog } from "../../components/ui/ConfirmDialog";
import { CopyText } from "../../components/ui/CopyButton";
import { Frame, SectionBar } from "../../components/ui/Frame";
import { useToast } from "../../components/ui/Toasts";
import { ru } from "../../i18n/ru";

const t = ru.configEditor;
const iconProps = { size: 16, strokeWidth: 2, strokeLinecap: "square" as const };
const ADDR = "ADMIN_ADDR";
const POLL_MS = 500;
const POLL_LIMIT = 60;

function sleep(ms: number) {
  return new Promise((r) => setTimeout(r, ms));
}

// After a same-address restart the old server dies and a new one comes up on the same origin.
async function waitForRestart(): Promise<boolean> {
  await sleep(800);
  for (let i = 0; i < POLL_LIMIT; i++) {
    try {
      const res = await fetch("/api/config", { cache: "no-store" });
      if (res.ok) return true;
    } catch {
      /* still restarting */
    }
    await sleep(POLL_MS);
  }
  return false;
}

// A new origin can't be read (no CORS), but an opaque no-cors response proves the server is up.
async function waitForOrigin(url: string): Promise<boolean> {
  await sleep(800);
  for (let i = 0; i < POLL_LIMIT; i++) {
    try {
      await fetch(`${url}/api/config`, { mode: "no-cors", cache: "no-store" });
      return true;
    } catch {
      /* not yet */
    }
    await sleep(POLL_MS);
  }
  return false;
}

function SourceBadge({ f }: { f: ConfigField }) {
  if (f.source !== "env" && f.source !== "secret" && f.source !== "flag") return null;
  return (
    <span className="badge badge-warning" title={t.sourceHint[f.source]}>
      <StatusGlyph kind="triangle" size={10} />
      {t.source[f.source]}
    </span>
  );
}

export function ConfigSection() {
  const qc = useQueryClient();
  const toast = useToast();
  const q = useQuery({ queryKey: ["config"], queryFn: api.config });
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [confirmMove, setConfirmMove] = useState(false);
  const [phase, setPhase] = useState<null | { kind: "restart" } | { kind: "move"; url: string } | { kind: "lost"; url: string }>(null);
  const view = q.data;

  useEffect(() => {
    if (view && phase === null) setDraft(Object.fromEntries(view.fields.map((f) => [f.key, f.value])));
  }, [view, phase]);

  const dirty = view ? view.fields.filter((f) => (draft[f.key] ?? "").trim() !== f.value) : [];
  const addrField = view?.fields.find((f) => f.key === ADDR);
  const newAddr = (draft[ADDR] ?? "").trim() || addrField?.default || "";
  const moving = Boolean(addrField && dirty.some((f) => f.key === ADDR) && newAddr !== view?.addr);

  const afterSave = async (res: ConfigSaved) => {
    if (res.changed.length === 0) {
      toast({ tone: "success", message: t.noChanges });
      return;
    }
    if (!res.restarting) {
      qc.setQueryData(["config"], res.config);
      toast({ tone: "success", message: t.savedManual, detail: res.changed.join(", ") });
      return;
    }
    if (res.changed.includes(ADDR) && res.url !== window.location.origin) {
      setPhase({ kind: "move", url: res.url });
      if (await waitForOrigin(res.url)) window.location.assign(`${res.url}/settings`);
      else setPhase({ kind: "lost", url: res.url });
      return;
    }
    setPhase({ kind: "restart" });
    const ok = await waitForRestart();
    setPhase(null);
    await qc.invalidateQueries();
    toast({ tone: ok ? "success" : "danger", message: ok ? t.applied : t.restartLost, detail: res.changed.join(", ") });
  };

  const save = useMutation({
    mutationFn: () => api.saveConfig(Object.fromEntries(dirty.map((f) => [f.key, (draft[f.key] ?? "").trim()]))),
    onSuccess: (res) => void afterSave(res),
    onError: (e) => toast({ tone: "danger", message: t.failed, detail: e instanceof ApiError ? e.message : String(e) }),
  });

  const submit = () => (moving ? setConfirmMove(true) : save.mutate());

  return (
    <Frame label={t.title}>
      <div className="settings-block">
        <SectionBar title={t.title} />
        {!view ? (
          <div className="skeleton-row" />
        ) : (
          <form
            className="config-form"
            onSubmit={(e) => {
              e.preventDefault();
              submit();
            }}
          >
            <p className="meta">
              {view.exists ? t.file : t.fileMissing} <CopyText value={view.path} className="mono" />
            </p>

            <div className="config-fields">
              {view.fields.map((f) => {
                const value = draft[f.key] ?? "";
                const changed = value.trim() !== f.value;
                return (
                  <label key={f.key} className={`field ${changed ? "is-changed" : ""}`}>
                    <span className="field-label">
                      {t.labels[f.key] ?? f.key} <span className="mono micro meta">{f.key}</span>
                    </span>
                    <span className="input">
                      <input
                        className="mono"
                        type={f.key === "MAX_WORKERS" ? "number" : "text"}
                        min={f.key === "MAX_WORKERS" ? 1 : undefined}
                        max={f.key === "MAX_WORKERS" ? 64 : undefined}
                        value={value}
                        placeholder={f.default ? t.placeholder(f.default) : ""}
                        spellCheck={false}
                        autoComplete="off"
                        disabled={save.isPending || phase !== null}
                        onChange={(e) => setDraft((d) => ({ ...d, [f.key]: e.target.value }))}
                      />
                    </span>
                    <span className="field-help">
                      {t.hints[f.key]}
                      {f.effective !== (f.value || f.default) && f.effective && <> · {t.effective(f.effective)}</>}
                    </span>
                    <SourceBadge f={f} />
                  </label>
                );
              })}
            </div>

            {moving && (
              <div className="notice notice-warning" role="alert">
                <StatusGlyph kind="triangle" size={12} />
                <span>{t.moveWarning(`http://${newAddr}`)}</span>
              </div>
            )}

            {view.otherKeys.length > 0 && <p className="meta micro">{t.otherKeys(view.otherKeys.join(", "))}</p>}
            <p className="meta micro">{view.canRestart ? t.restartNote : t.manualNote}</p>
            <p className="meta micro">{t.buildNote}</p>

            <div className="config-actions">
              <Button
                type="button"
                variant="ghost"
                icon={<RotateCcw {...iconProps} />}
                disabled={dirty.length === 0 || save.isPending || phase !== null}
                onClick={() => setDraft(Object.fromEntries(view.fields.map((f) => [f.key, f.value])))}
              >
                {t.reset}
              </Button>
              <Button
                type="submit"
                variant={moving ? "danger" : "primary"}
                icon={<Save {...iconProps} />}
                loading={save.isPending || phase?.kind === "restart"}
                loadingLabel={phase?.kind === "restart" ? t.restarting : undefined}
                disabled={dirty.length === 0 || phase !== null}
              >
                {view.canRestart ? t.saveRestart : t.save}
              </Button>
            </div>

            {phase?.kind === "move" && (
              <div className="notice notice-warning" role="status">
                <span className="spin">
                  <StatusGlyph kind="ring" size={12} />
                </span>
                <span>{t.moving(phase.url)}</span>
              </div>
            )}
            {phase?.kind === "lost" && (
              <div className="notice notice-warning" role="alert">
                <StatusGlyph kind="fault" size={12} />
                <span>
                  {t.movedLost}{" "}
                  <a className="mono" href={`${phase.url}/settings`}>
                    {phase.url}
                  </a>
                </span>
              </div>
            )}
          </form>
        )}
      </div>

      <ConfirmDialog
        open={confirmMove}
        onOpenChange={setConfirmMove}
        danger
        title={t.moveTitle}
        body={
          <>
            <p>{t.moveWarning(`http://${newAddr}`)}</p>
            <p className="meta">{t.moveDetail}</p>
          </>
        }
        confirmLabel={t.moveConfirm}
        confirmIcon={<Save {...iconProps} />}
        onConfirm={() => save.mutate()}
      />
    </Frame>
  );
}
