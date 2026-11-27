import { useMemo, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, RotateCw, Trash2 } from "lucide-react";
import { ApiError, api } from "../../api/client";
import type { AddPortRequest, PortRow, PortsView, Run } from "../../api/types";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { Button } from "../../components/ui/Button";
import { Checkbox } from "../../components/ui/Checkbox";
import { ConfirmDialog } from "../../components/ui/ConfirmDialog";
import { Frame, SectionBar } from "../../components/ui/Frame";
import { useToast } from "../../components/ui/Toasts";
import { ru } from "../../i18n/ru";
import { usePref, type Route } from "../../lib/hooks";
import { NetworkTabs } from "../network/NetworkTabs";
import { StatusBadge } from "../containers/StatusBadge";

const iconProps = { size: 14, strokeWidth: 2, strokeLinecap: "square" as const };
const t = ru.portsPage;

function errorText(e: unknown): string {
  return e instanceof ApiError ? e.message : String(e);
}

function slug(s: string, port: number): string {
  const v = s
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, "_")
    .replace(/^[_-]+|[_-]+$/g, "");
  return v || `tcp_${port}`;
}

function hostAddr(p: PortRow): string {
  return `${p.hostIp || "0.0.0.0"}:${p.hostPort}`;
}

interface Props {
  bulkBusy: boolean;
  onRunStarted: (r: Run) => void;
  go: (r: Route) => void;
  href: (r: Route) => string;
}

export function PortsPage({ bulkBusy, onRunStarted, go, href }: Props) {
  const qc = useQueryClient();
  const toast = useToast();
  const q = useQuery({ queryKey: ["ports"], queryFn: api.ports, refetchInterval: bulkBusy ? 2000 : 5000 });
  const [skipApply, setSkipApply] = usePref("confirm.applyPorts");
  const [applyOpen, setApplyOpen] = useState(false);
  const [removing, setRemoving] = useState<PortRow | null>(null);
  const [removeEp, setRemoveEp] = useState(false);

  const setView = (v: PortsView) => qc.setQueryData(["ports"], v);

  const apply = useMutation({
    mutationFn: api.applyPorts,
    onSuccess: (r) => {
      onRunStarted(r);
      void qc.invalidateQueries({ queryKey: ["ports"] });
    },
    onError: (e) => toast({ tone: "danger", message: t.apply, detail: errorText(e) }),
  });

  const remove = useMutation({
    mutationFn: ({ p, ep }: { p: PortRow; ep: boolean }) => api.removePort(p.index, p.raw, ep),
    onSuccess: (v) => {
      setView(v);
      toast({ tone: "success", message: t.removed });
    },
    onError: (e) => toast({ tone: "danger", message: t.remove, detail: errorText(e) }),
  });

  const view = q.data;

  if (!view) {
    return (
      <main className="page" id="main">
        <SectionBar title={t.title} jp={t.titleJp} level={1} />
        <NetworkTabs active="ports" go={go} href={href} />
        {q.isError ? (
          <div className="empty">
            <StatusGlyph kind="fault" size={32} />
            <div className="empty-code">{ru.error.title}</div>
            <p className="mono">{errorText(q.error)}</p>
            <Button variant="outline" onClick={() => void q.refetch()}>
              {ru.error.retry}
            </Button>
          </div>
        ) : (
          <div className="skeleton-rows">
            {Array.from({ length: 6 }, (_, i) => (
              <div key={i} className="skeleton-row" />
            ))}
          </div>
        )}
      </main>
    );
  }

  const live = view.ports.filter((p) => p.live).length;
  const removingEp = removing ? view.entryPoints.find((e) => e.name === removing.entryPoint) : undefined;

  return (
    <main className="page" id="main">
      <header className="page-head">
        <SectionBar title={t.title} jp={t.titleJp} level={1} count={`${view.ports.length} PORTS`} />
        <NetworkTabs active="ports" go={go} href={href} />
        <p className="page-summary">
          {t.summary(live)} ·{" "}
          <span className="mono micro" title={view.composeFile}>
            {view.composeFile}
          </span>{" "}
          ·{" "}
          <span className="mono micro" title={view.traefikConfig}>
            {view.traefikConfig}
          </span>
          {view.container ? <StatusBadge container={view.container} /> : <span className="badge badge-danger">{t.noTraefik}</span>}
        </p>
      </header>

      {view.warnings.map((w) => (
        <div key={w} className="notice notice-warning">
          <StatusGlyph kind="triangle" size={12} />
          <span>{w}</span>
        </div>
      ))}

      {view.needsApply && (
        <div className="notice notice-apply" role="status">
          <div className="hazard-band hazard-warning" aria-hidden />
          <div className="notice-text">
            <div className="notice-title">{t.applyTitle}</div>
            <ul className="meta">
              {view.applyReasons.map((r) => (
                <li key={r}>{r}</li>
              ))}
            </ul>
          </div>
          <Button
            variant="primary"
            icon={<RotateCw {...iconProps} />}
            loading={apply.isPending}
            loadingLabel={ru.bulk.gerund.restart}
            disabled={bulkBusy}
            disabledReason={ru.bulk.busy}
            onClick={() => (skipApply ? apply.mutate() : setApplyOpen(true))}
          >
            {t.apply}
          </Button>
        </div>
      )}

      <Frame flush label={t.title}>
        <div className="table-scroll">
          <table className="table table-ports">
            <thead>
              <tr>
                <th scope="col">{t.columns.status}</th>
                <th scope="col">{t.columns.host}</th>
                <th scope="col">{t.columns.target}</th>
                <th scope="col">{t.columns.comment}</th>
                <th scope="col">{t.columns.entryPoint}</th>
                <th scope="col">{t.columns.consumers}</th>
                <th scope="col" className="th-actions">
                  <span className="visually-hidden">{ru.columns.actions}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {view.ports.map((p) => (
                <tr key={`${p.index}-${p.raw}`} className={`row ${p.parsed && !p.live ? "row-pending" : ""}`}>
                  <td data-label={t.columns.status}>
                    {!p.parsed ? (
                      <span className="badge badge-neutral">
                        <StatusGlyph kind="dashed" size={10} />
                        {t.unparsed}
                      </span>
                    ) : p.live ? (
                      <span className="badge badge-success">
                        <StatusGlyph kind="square" size={10} />
                        {t.live}
                      </span>
                    ) : (
                      <span className="badge badge-warning">
                        <StatusGlyph kind="triangle" size={10} />
                        {t.pending}
                      </span>
                    )}
                  </td>
                  <td data-label={t.columns.host} className="mono">
                    {p.parsed ? hostAddr(p) : p.raw}
                  </td>
                  <td data-label={t.columns.target} className="mono">
                    {p.parsed ? `${p.containerPort}/${p.protocol}` : "—"}
                  </td>
                  <td data-label={t.columns.comment}>{p.comment || <span className="meta">—</span>}</td>
                  <td data-label={t.columns.entryPoint} className="mono">
                    {p.entryPoint || <span className="meta">{t.noEntryPoint}</span>}
                  </td>
                  <td data-label={t.columns.consumers}>
                    {p.consumers.length === 0 ? (
                      <span className="meta">—</span>
                    ) : (
                      p.consumers.map((c) => (
                        <div key={`${c.container}-${c.router}`} className="mono micro" title={`${c.kind} router ${c.router}`}>
                          {c.container}
                        </div>
                      ))
                    )}
                  </td>
                  <td className="cell-actions">
                    <div className="row-actions">
                      <Button
                        size="sm"
                        variant="danger-outline"
                        iconOnly
                        title={t.remove}
                        icon={<Trash2 {...iconProps} />}
                        disabled={remove.isPending}
                        onClick={() => {
                          setRemoveEp(false);
                          setRemoving(p);
                        }}
                      >
                        {t.remove}
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Frame>

      <AddPortForm view={view} onDone={setView} />

      <Frame flush label={t.entryPoints.title}>
        <div className="frame-pad">
          <SectionBar title={t.entryPoints.title} jp={t.entryPoints.jp} count={`${view.entryPoints.length}`} />
        </div>
        <div className="table-scroll">
          <table className="table">
            <thead>
              <tr>
                <th scope="col">{t.entryPoints.name}</th>
                <th scope="col">{t.entryPoints.address}</th>
                <th scope="col">{t.entryPoints.published}</th>
                <th scope="col">{t.entryPoints.consumers}</th>
              </tr>
            </thead>
            <tbody>
              {view.entryPoints.map((e) => (
                <tr key={e.name} className="row">
                  <td className="mono" data-label={t.entryPoints.name}>
                    {e.name}
                  </td>
                  <td className="mono" data-label={t.entryPoints.address}>
                    {e.address}
                  </td>
                  <td data-label={t.entryPoints.published}>
                    {e.published ? (
                      <span className="tone-success">
                        <StatusGlyph kind="square" size={10} label={t.entryPoints.published} />
                      </span>
                    ) : (
                      <span className="tone-warning meta" title={t.entryPoints.notPublished}>
                        <StatusGlyph kind="triangle" size={10} /> {t.entryPoints.notPublished}
                      </span>
                    )}
                  </td>
                  <td data-label={t.entryPoints.consumers}>
                    {e.consumers.length === 0 ? (
                      <span className="meta">{t.entryPoints.unused}</span>
                    ) : (
                      e.consumers.map((c) => (
                        <span key={`${c.container}-${c.router}`} className="chip mono micro" title={`${c.kind} router ${c.router}`}>
                          {c.container}
                        </span>
                      ))
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Frame>

      <ConfirmDialog
        open={applyOpen}
        onOpenChange={setApplyOpen}
        title={t.applyConfirmTitle}
        body={<p>{t.applyConfirmBody}</p>}
        confirmLabel={t.apply}
        confirmIcon={<RotateCw {...iconProps} />}
        cli={`docker compose --file ${view.composeFile} up -d --force-recreate --no-deps ${view.service}`}
        offerDontAsk
        onConfirm={(dontAsk) => {
          if (dontAsk) setSkipApply(true);
          apply.mutate();
        }}
      />

      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(o) => !o && setRemoving(null)}
        danger
        title={removing ? t.removeTitle(removing.raw) : ""}
        body={<p>{t.removeBody}</p>}
        extra={
          removing?.entryPoint && removingEp ? (
            removingEp.consumers.length > 0 ? (
              <p className="meta tone-warning">{t.entryPointBusy(removingEp.name, removingEp.consumers.map((c) => c.container).join(", "))}</p>
            ) : (
              <Checkbox checked={removeEp} onChange={setRemoveEp}>
                {t.removeEntryPoint(removingEp.name)}
              </Checkbox>
            )
          ) : null
        }
        confirmLabel={t.remove}
        confirmIcon={<Trash2 {...iconProps} />}
        onConfirm={() => {
          if (removing) remove.mutate({ p: removing, ep: removeEp });
        }}
      />
    </main>
  );
}

function AddPortForm({ view, onDone }: { view: PortsView; onDone: (v: PortsView) => void }) {
  const toast = useToast();
  const f = t.form;
  const [hostIp, setHostIp] = useState("127.0.0.1");
  const [hostPort, setHostPort] = useState("");
  const [targetPort, setTargetPort] = useState("");
  const [protocol, setProtocol] = useState("tcp");
  const [comment, setComment] = useState("");
  const [createEp, setCreateEp] = useState(true);
  const [epName, setEpName] = useState("");
  const [error, setError] = useState("");

  const hp = Number(hostPort);
  const tp = targetPort ? Number(targetPort) : hp;
  const existingEp = useMemo(() => view.entryPoints.find((e) => e.port === tp && e.protocol === protocol), [view.entryPoints, tp, protocol]);
  const effectiveEpName = epName || slug(comment, tp || 0);
  const canCreateEp = view.traefikConfigFound && !existingEp;

  const add = useMutation({
    mutationFn: (req: AddPortRequest) => api.addPort(req),
    onSuccess: (v) => {
      onDone(v);
      toast({ tone: "success", message: f.title, detail: t.added });
      setHostPort("");
      setTargetPort("");
      setComment("");
      setEpName("");
      setError("");
    },
    onError: (e) => setError(errorText(e)),
  });

  const valid = Number.isInteger(hp) && hp > 0 && hp < 65536 && Number.isInteger(tp) && tp > 0 && tp < 65536;
  const spec = valid ? `${hostIp ? `${hostIp}:` : ""}${hp}:${tp}/${protocol}${comment ? `  # ${comment}` : ""}` : "";

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!valid) return;
    add.mutate({
      hostIp,
      hostPort: hp,
      containerPort: tp,
      protocol,
      comment: comment.trim(),
      entryPoint: createEp && canCreateEp ? effectiveEpName : "",
    });
  };

  return (
    <Frame label={f.title}>
      <form className="form" onSubmit={submit}>
        <SectionBar title={f.title} jp={f.titleJp} />
        <div className="form-grid">
          <label className="field">
            <span className="field-label">{f.ip}</span>
            <span className="input select">
              <select value={hostIp} onChange={(e) => setHostIp(e.target.value)}>
                <option value="127.0.0.1">{f.ipLocal}</option>
                <option value="0.0.0.0">{f.ipAll}</option>
              </select>
            </span>
          </label>
          <label className="field">
            <span className="field-label">
              {f.hostPort} <span className="field-req">· {f.required}</span>
            </span>
            <span className="input">
              <input inputMode="numeric" className="mono" value={hostPort} onChange={(e) => setHostPort(e.target.value.replace(/\D/g, ""))} placeholder="25433" required />
            </span>
          </label>
          <label className="field">
            <span className="field-label">{f.targetPort}</span>
            <span className="input">
              <input inputMode="numeric" className="mono" value={targetPort} onChange={(e) => setTargetPort(e.target.value.replace(/\D/g, ""))} placeholder={hostPort || "25433"} />
            </span>
            <span className="field-help">{f.targetHint}</span>
          </label>
          <label className="field">
            <span className="field-label">{f.protocol}</span>
            <span className="input select">
              <select value={protocol} onChange={(e) => setProtocol(e.target.value)}>
                <option value="tcp">tcp</option>
                <option value="udp">udp</option>
              </select>
            </span>
          </label>
          <label className="field field-wide">
            <span className="field-label">{f.comment}</span>
            <span className="input">
              <input value={comment} onChange={(e) => setComment(e.target.value)} placeholder={f.commentHint} maxLength={80} />
            </span>
          </label>
        </div>

        <div className="form-ep">
          {existingEp ? (
            <p className="meta">{f.existing(existingEp.name)}</p>
          ) : (
            <>
              <Checkbox checked={createEp && canCreateEp} disabled={!canCreateEp} onChange={setCreateEp}>
                {f.createEntryPoint}
              </Checkbox>
              {createEp && canCreateEp && (
                <label className="field">
                  <span className="field-label">{f.entryPointName}</span>
                  <span className="input">
                    <input className="mono" value={epName} onChange={(e) => setEpName(e.target.value)} placeholder={effectiveEpName} pattern="[A-Za-z0-9][A-Za-z0-9_\-]*" />
                  </span>
                  <span className="field-help">{f.entryPointHint}</span>
                </label>
              )}
            </>
          )}
        </div>

        {spec && (
          <p className="form-preview">
            <span className="meta">{f.preview}</span> <code>- "{spec}"</code>
            {createEp && canCreateEp && (
              <>
                {" "}
                <span className="meta">+ entryPoint</span> <code>{effectiveEpName}: ":{tp}/{protocol}"</code>
              </>
            )}
          </p>
        )}
        {error && (
          <p className="form-error" role="alert">
            <StatusGlyph kind="fault" size={12} /> {error}
          </p>
        )}
        <div className="form-actions">
          <Button type="submit" variant="outline" icon={<Plus {...iconProps} />} loading={add.isPending} disabled={!valid}>
            {f.submit}
          </Button>
        </div>
      </form>
    </Frame>
  );
}
