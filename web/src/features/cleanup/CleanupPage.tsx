import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { DatabaseBackup, Eraser, RefreshCw, ShieldCheck } from "lucide-react";
import { ApiError, api } from "../../api/client";
import type { CacheMode, CleanupPreview, CleanupResult } from "../../api/types";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { Button } from "../../components/ui/Button";
import { Checkbox } from "../../components/ui/Checkbox";
import { ConfirmDialog } from "../../components/ui/ConfirmDialog";
import { CopyText } from "../../components/ui/CopyButton";
import { Frame, SectionBar } from "../../components/ui/Frame";
import { useToast } from "../../components/ui/Toasts";
import { ru } from "../../i18n/ru";
import { rf } from "../../i18n/ru.features";
import { absolute } from "../../lib/format";
import { bytes } from "../../lib/metrics";

const t = ru.cleanup;
const iconProps = { size: 16, strokeWidth: 2, strokeLinecap: "square" as const };
const VOLUMES_CMD = "docker volume ls -f dangling=true";

function shortId(id: string): string {
  return id.replace(/^sha256:/, "").slice(0, 12);
}

function errorText(e: unknown): string {
  return e instanceof ApiError ? e.message : String(e);
}

function toggle(set: Set<string>, id: string, on: boolean): Set<string> {
  const next = new Set(set);
  if (on) next.add(id);
  else next.delete(id);
  return next;
}

function ResultPanel({ result }: { result: CleanupResult }) {
  const ok = result.items.filter((i) => i.ok).length;
  return (
    <Frame label={t.resultTitle}>
      <div className="settings-block">
        <SectionBar title={t.resultTitle} count={t.resultSummary(ok, result.items.length - ok)} />
        <p className="mon-value mono">{t.freed(bytes(result.freed))}</p>
        <ul className="cleanup-result">
          {result.items.map((it) => (
            <li key={`${it.kind}-${it.id}`} className={it.ok ? "" : "tone-danger"}>
              <StatusGlyph kind={it.ok ? "square" : "fault"} size={10} />
              <span className="meta micro">{t.kinds[it.kind]}</span>
              <span className="mono micro">{it.kind === "buildCache" ? `${t.cacheModes[it.id]} · ${t.cacheDeleted(it.name)}` : it.name}</span>
              <span className="mono micro">{it.ok ? bytes(it.size) : it.error}</span>
            </li>
          ))}
        </ul>
      </div>
    </Frame>
  );
}

export function CleanupPage({ bulkBusy, onGoBackups }: { bulkBusy: boolean; onGoBackups: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const q = useQuery({ queryKey: ["cleanup"], queryFn: api.cleanupPreview, refetchOnWindowFocus: false, staleTime: 30_000 });
  const [images, setImages] = useState<Set<string>>(new Set());
  const [containers, setContainers] = useState<Set<string>>(new Set());
  const [cache, setCache] = useState<CacheMode | "">("");
  const [confirm, setConfirm] = useState(false);
  const [result, setResult] = useState<CleanupResult | null>(null);
  const p: CleanupPreview | undefined = q.data;

  // Drop selections that vanished after a rescan; preselect dangling images on the first scan.
  const [seeded, setSeeded] = useState(false);
  useEffect(() => {
    if (!p) return;
    const imageIds = new Set(p.images.map((i) => i.id));
    const freeContainers = new Set(p.containers.filter((c) => !c.protected).map((c) => c.id));
    if (!seeded) {
      setImages(new Set(p.images.filter((i) => i.dangling).map((i) => i.id)));
      setSeeded(true);
    } else {
      setImages((s) => new Set([...s].filter((id) => imageIds.has(id))));
    }
    setContainers((s) => new Set([...s].filter((id) => freeContainers.has(id))));
  }, [p, seeded]);

  const run = useMutation({
    mutationFn: api.cleanupRun,
    onSuccess: (res) => {
      setResult(res);
      const fails = res.items.filter((i) => !i.ok).length;
      toast({ tone: fails > 0 ? "danger" : "success", message: t.done, detail: `${t.freed(bytes(res.freed))} · ${t.resultSummary(res.items.length - fails, fails)}` });
      void qc.invalidateQueries({ queryKey: ["cleanup"] });
      void qc.invalidateQueries({ queryKey: ["state"] });
      void qc.invalidateQueries({ queryKey: ["metrics-host"] });
    },
    onError: (e) => toast({ tone: "danger", message: t.failed, detail: errorText(e) }),
  });

  const bucket = p?.buildCache.buckets.find((b) => b.mode === cache);
  const estimate = useMemo(() => {
    if (!p) return 0;
    const im = p.images.filter((i) => images.has(i.id)).reduce((a, i) => a + i.unique, 0);
    const ct = p.containers.filter((c) => containers.has(c.id)).reduce((a, c) => a + c.size, 0);
    return im + ct + (bucket?.size ?? 0);
  }, [p, images, containers, bucket]);

  const parts = [
    images.size > 0 ? t.partImages(images.size) : "",
    containers.size > 0 ? t.partContainers(containers.size) : "",
    cache ? t.partCache(t.cacheModes[cache]!.toLowerCase()) : "",
  ].filter(Boolean);

  const freeContainers = p?.containers.filter((c) => !c.protected) ?? [];
  const allImages = p !== undefined && p.images.length > 0 && images.size === p.images.length;
  const allContainers = freeContainers.length > 0 && containers.size === freeContainers.length;

  return (
    <main className="page" id="main">
      <header className="page-head">
        <SectionBar title={t.title} jp={t.titleJp} level={1} count={p ? t.scannedAt(absolute(p.scannedAt)) : ""}>
          <Button size="sm" variant="ghost" icon={<RefreshCw {...iconProps} />} loading={q.isFetching} onClick={() => void q.refetch()}>
            {t.rescan}
          </Button>
        </SectionBar>
      </header>
      <div className="notice">
        <DatabaseBackup size={14} strokeWidth={2} strokeLinecap="square" aria-hidden />
        <span className="notice-text">{rf.backups.beforeCleanup}</span>
        <Button size="sm" variant="outline" onClick={onGoBackups}>
          {rf.backups.goBackups}
        </Button>
      </div>

      <div className="notice notice-safe" role="note">
        <ShieldCheck {...iconProps} aria-hidden />
        <div className="notice-text">
          <div className="notice-title">{t.safeTitle}</div>
          <ul>
            {t.safe.map((line) => (
              <li key={line}>{line}</li>
            ))}
          </ul>
          <p className="meta micro">{t.safeNote}</p>
        </div>
      </div>

      {q.isError && (
        <div className="notice notice-warning" role="alert">
          <StatusGlyph kind="fault" size={12} />
          <span className="mono micro">{errorText(q.error)}</span>
        </div>
      )}

      {!p ? (
        <Frame label={t.title}>
          <div className="settings-block">
            <p className="meta">{q.isLoading ? t.scanning : ""}</p>
            <div className="skeleton-row" />
          </div>
        </Frame>
      ) : (
        <>
          {result && <ResultPanel result={result} />}

          <Frame label={t.cache}>
            <div className="settings-block">
              <SectionBar title={t.cache} count={t.cacheTotal(bytes(p.buildCache.total), p.buildCache.count)} />
              <p className="meta">{t.cacheHint}</p>
              <div className="cache-modes" role="radiogroup" aria-label={t.cache}>
                {(["", ...p.buildCache.buckets.map((b) => b.mode)] as (CacheMode | "")[]).map((mode) => {
                  const b = p.buildCache.buckets.find((x) => x.mode === mode);
                  return (
                    <label key={mode || "none"} className={`cache-mode ${cache === mode ? "is-active" : ""}`}>
                      <input type="radio" name="cache-mode" checked={cache === mode} onChange={() => setCache(mode)} />
                      <span>{t.cacheModes[mode || "none"]}</span>
                      <span className="mono micro">{b ? bytes(b.size) : "0 Б"}</span>
                    </label>
                  );
                })}
              </div>
              {p.buildCache.inUse > 0 && <p className="meta micro">{t.cacheInUse(bytes(p.buildCache.inUse))}</p>}
            </div>
          </Frame>

          <Frame flush label={t.images}>
            <div className="frame-pad">
              <SectionBar title={t.images} count={bytes(p.images.reduce((a, i) => a + i.unique, 0))}>
                <Button size="sm" variant="ghost" disabled={p.images.length === 0} onClick={() => setImages(new Set(p.images.filter((i) => i.dangling).map((i) => i.id)))}>
                  {t.selectDangling}
                </Button>
                <Button size="sm" variant="ghost" disabled={p.images.length === 0} onClick={() => setImages(allImages ? new Set() : new Set(p.images.map((i) => i.id)))}>
                  {allImages ? t.selectNone : t.selectAll}
                </Button>
              </SectionBar>
              <p className="meta">{t.imagesHint}</p>
            </div>
            {p.images.length === 0 ? (
              <p className="meta frame-pad">{t.noImages}</p>
            ) : (
              <div className="table-scroll">
                <table className="table table-cleanup">
                  <thead>
                    <tr>
                      <th scope="col" className="th-check">
                        <span className="visually-hidden">{t.col.select}</span>
                      </th>
                      <th scope="col">{t.col.image}</th>
                      <th scope="col">{t.col.project}</th>
                      <th scope="col">{t.col.created}</th>
                      <th scope="col" className="num" title={t.sizeHint}>
                        {t.col.size}
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {p.images.map((im) => (
                      <tr key={im.id} className={`row ${images.has(im.id) ? "is-selected" : ""}`}>
                        <td className="cell-check">
                          <Checkbox checked={images.has(im.id)} onChange={(v) => setImages((s) => toggle(s, im.id, v))}>
                            <span className="visually-hidden">{im.tags[0] ?? shortId(im.id)}</span>
                          </Checkbox>
                        </td>
                        <td data-label={t.col.image}>
                          {im.dangling ? (
                            <>
                              <span className="badge badge-neutral">{t.dangling}</span> <span className="mono micro">{shortId(im.id)}</span>
                            </>
                          ) : (
                            im.tags.map((tag) => (
                              <div key={tag} className="mono micro cleanup-name">
                                {tag}
                              </div>
                            ))
                          )}
                        </td>
                        <td data-label={t.col.project} className="mono micro">
                          {im.project ? [im.project, im.service].filter(Boolean).join(" / ") : <span className="meta">—</span>}
                        </td>
                        <td data-label={t.col.created} className="mono micro">
                          {absolute(im.created)}
                        </td>
                        <td data-label={t.col.size} className="num mono micro" title={`${t.sizeHint}. Полный размер ${bytes(im.size)}`}>
                          {bytes(im.unique)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Frame>

          <Frame flush label={t.containers}>
            <div className="frame-pad">
              <SectionBar title={t.containers} count={`${p.containers.length}`}>
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={freeContainers.length === 0}
                  onClick={() => setContainers(allContainers ? new Set() : new Set(freeContainers.map((c) => c.id)))}
                >
                  {allContainers ? t.selectNone : t.selectAll}
                </Button>
              </SectionBar>
              <p className="meta">{t.containersHint}</p>
            </div>
            {p.containers.length === 0 ? (
              <p className="meta frame-pad">{t.noContainers}</p>
            ) : (
              <div className="table-scroll">
                <table className="table table-cleanup">
                  <thead>
                    <tr>
                      <th scope="col" className="th-check">
                        <span className="visually-hidden">{t.col.select}</span>
                      </th>
                      <th scope="col">{t.col.name}</th>
                      <th scope="col">{t.col.project}</th>
                      <th scope="col">{t.col.state}</th>
                      <th scope="col" className="num">
                        {t.col.size}
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {p.containers.map((c) => (
                      <tr key={c.id} className={`row ${c.protected ? "row-protected" : ""} ${containers.has(c.id) ? "is-selected" : ""}`}>
                        <td className="cell-check">
                          <Checkbox checked={containers.has(c.id)} disabled={c.protected} onChange={(v) => setContainers((s) => toggle(s, c.id, v))}>
                            <span className="visually-hidden">{c.name}</span>
                          </Checkbox>
                        </td>
                        <td data-label={t.col.name}>
                          <div className="mono cleanup-name">{c.name}</div>
                          <div className="meta micro mono">{c.image}</div>
                          {c.protected && (
                            <span className="badge badge-success cleanup-shield" title={c.reason === "volume" ? t.reasons.volume(c.volumes.join(", ")) : t.reasons.database}>
                              <ShieldCheck size={12} strokeWidth={2} aria-hidden />
                              {t.protected} · {c.reason === "volume" ? t.reasons.volume(c.volumes.map(shortId).join(", ")) : t.reasons.database}
                            </span>
                          )}
                        </td>
                        <td data-label={t.col.project} className="mono micro">
                          {c.project ? [c.project, c.service].filter(Boolean).join(" / ") : <span className="meta">{t.outside}</span>}
                        </td>
                        <td data-label={t.col.state} className="micro">
                          {c.status || c.state}
                        </td>
                        <td data-label={t.col.size} className="num mono micro">
                          {bytes(c.size)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Frame>

          <Frame label={t.volumes}>
            <div className="settings-block">
              <SectionBar title={t.volumes} count={bytes(p.volumes.size)}>
                <span className="badge badge-success">
                  <ShieldCheck size={12} strokeWidth={2} aria-hidden />
                  {t.volumesNever}
                </span>
              </SectionBar>
              <p className="meta">{t.volumesHint(p.volumes.count, bytes(p.volumes.size), p.volumes.unused, bytes(p.volumes.unusedSize))}</p>
              <p className="meta micro">
                {t.volumesManual}: <CopyText value={VOLUMES_CMD} className="mono" />
              </p>
            </div>
          </Frame>

          <div className="cleanup-bar" role="region" aria-label={t.selected}>
            <div>
              <div className="meta micro">{t.selected}</div>
              <div className="mon-value mono">{parts.length > 0 ? t.selectedApprox(bytes(estimate)) : "—"}</div>
              <div className="meta micro">{parts.length > 0 ? parts.join(" · ") : t.nothing}</div>
            </div>
            <Button
              variant="danger"
              icon={<Eraser {...iconProps} />}
              loading={run.isPending}
              disabled={parts.length === 0 || bulkBusy || run.isPending}
              disabledReason={bulkBusy ? ru.bulk.busy : undefined}
              onClick={() => setConfirm(true)}
            >
              {run.isPending ? t.running : t.run}
            </Button>
          </div>
        </>
      )}

      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        danger
        title={t.confirmTitle}
        body={
          <>
            <p>{t.confirmBody(parts)}</p>
            <p className="meta">{t.confirmRestore}</p>
            <p className="mon-value mono">{t.selectedApprox(bytes(estimate))}</p>
          </>
        }
        confirmLabel={t.run}
        confirmIcon={<Eraser {...iconProps} />}
        onConfirm={() => run.mutate({ images: [...images], containers: [...containers], buildCache: cache })}
      />
    </main>
  );
}
