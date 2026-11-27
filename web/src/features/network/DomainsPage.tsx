import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FileLock2, ShieldCheck } from "lucide-react";
import { ApiError, api } from "../../api/client";
import type { DomainHost, DomainsView } from "../../api/types";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { Button } from "../../components/ui/Button";
import { CopyText } from "../../components/ui/CopyButton";
import { Frame, SectionBar } from "../../components/ui/Frame";
import { useToast } from "../../components/ui/Toasts";
import { ru } from "../../i18n/ru";
import { rf } from "../../i18n/ru.features";
import { absolute } from "../../lib/format";
import type { Route } from "../../lib/hooks";
import { NetworkTabs } from "./NetworkTabs";

const t = rf.domains;
const iconProps = { size: 14, strokeWidth: 2, strokeLinecap: "square" as const };

function errorText(e: unknown): string {
  return e instanceof ApiError ? e.message : String(e);
}

function Resolution({ h }: { h: DomainHost }) {
  if (h.autoLocal && !h.resolves) return <span className="meta">{t.autoLocal}</span>;
  if (!h.resolves)
    return (
      <span className="tone-danger inline-status">
        <StatusGlyph kind="fault" size={10} /> {t.nowhere}
      </span>
    );
  if (h.local)
    return (
      <span className="tone-success inline-status">
        <StatusGlyph kind="square" size={10} /> {t.local}
      </span>
    );
  return (
    <span className="tone-warning inline-status" title={h.addrs.join(", ")}>
      <StatusGlyph kind="triangle" size={10} /> {t.elsewhere(h.addrs[0] ?? "")}
    </span>
  );
}

export function DomainsPage({ go, href }: { go: (r: Route) => void; href: (r: Route) => string }) {
  const qc = useQueryClient();
  const toast = useToast();
  const q = useQuery({ queryKey: ["domains"], queryFn: api.domains, refetchOnWindowFocus: false, staleTime: 15_000 });
  const setView = (v: DomainsView) => qc.setQueryData(["domains"], v);

  const hosts = useMutation({
    mutationFn: api.writeHosts,
    onSuccess: (v) => {
      setView(v);
      if (v.written) toast({ tone: "success", message: t.written });
    },
    onError: (e) => toast({ tone: "danger", message: t.write, detail: errorText(e) }),
  });
  const cert = useMutation({
    mutationFn: api.issueCert,
    onSuccess: (r) => {
      setView(r.domains);
      toast({ tone: "success", message: t.issued, detail: r.output.trim().split("\n").slice(-2).join(" ") });
    },
    onError: (e) => toast({ tone: "danger", message: t.issue, detail: errorText(e) }),
  });

  const v = hosts.data?.command ? hosts.data : q.data;
  const tls = v?.tls;

  return (
    <main className="page" id="main">
      <header className="page-head">
        <SectionBar title={rf.network.title} jp={rf.network.titleJp} level={1} />
        <NetworkTabs active="domains" go={go} href={href} />
      </header>

      {!v ? (
        q.isError ? (
          <div className="empty">
            <StatusGlyph kind="fault" size={32} />
            <p className="mono">{errorText(q.error)}</p>
            <Button variant="outline" onClick={() => void q.refetch()}>
              {ru.error.retry}
            </Button>
          </div>
        ) : (
          <div className="skeleton-rows">
            {Array.from({ length: 5 }, (_, i) => (
              <div key={i} className="skeleton-row" />
            ))}
          </div>
        )
      ) : (
        <>
          <Frame flush label={t.hosts}>
            <div className="frame-pad">
              <SectionBar title={t.hosts} count={t.hostsSummary(v.hosts.length, v.pending.length)}>
                <Button variant="primary" icon={<FileLock2 {...iconProps} />} loading={hosts.isPending} disabled={v.hosts.length === 0} onClick={() => hosts.mutate()}>
                  {t.write}
                </Button>
              </SectionBar>
              <p className="meta">
                {t.hostsFile}: <CopyText value={v.hostsFile} className="mono" />
              </p>
              {v.command && (
                <div className="notice notice-warning">
                  <span className="notice-text">{t.elevation}</span>
                  <CopyText value={v.command} className="mono" />
                </div>
              )}
            </div>
            {v.hosts.length === 0 ? (
              <p className="meta frame-pad">{t.empty}</p>
            ) : (
              <div className="table-scroll">
                <table className="table">
                  <thead>
                    <tr>
                      <th>{t.columns.host}</th>
                      <th>{t.columns.resolves}</th>
                      <th>{t.columns.hosts}</th>
                      <th>{t.columns.tls}</th>
                      <th>{t.columns.containers}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {v.hosts.map((h) => (
                      <tr key={h.name} className={!h.resolves && !h.autoLocal ? "is-fault" : ""}>
                        <td className="mono">{h.name}</td>
                        <td>
                          <Resolution h={h} />
                        </td>
                        <td className="meta">{h.inBlock ? t.inBlock : t.notInBlock}</td>
                        <td>
                          <StatusGlyph kind={h.inCert ? "square" : "outline"} size={10} label={h.inCert ? t.covered : t.noCert} />
                        </td>
                        <td className="mono micro">{h.containers.join(", ")}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Frame>

          {tls && (
            <Frame label={t.tls}>
              <div className="settings-block">
                <SectionBar title={t.tls}>
                  <Button
                    variant="outline"
                    icon={<ShieldCheck {...iconProps} />}
                    loading={cert.isPending}
                    disabled={!tls.mkcert || v.hosts.length === 0}
                    disabledReason={!tls.mkcert ? t.mkcertMissing : undefined}
                    onClick={() => cert.mutate()}
                  >
                    {tls.exists ? t.reissue : t.issue}
                  </Button>
                </SectionBar>
                {!tls.mkcert ? (
                  <p className="tone-warning">
                    {t.mkcertMissing} <CopyText value={t.mkcertInstall} className="mono" />
                  </p>
                ) : tls.caReady ? (
                  <p className="tone-success inline-status">
                    <StatusGlyph kind="square" size={10} /> {t.caReady} · <span className="mono micro">{tls.caRoot}</span>
                  </p>
                ) : (
                  <p className="tone-warning">
                    {t.caMissing} <CopyText value={tls.installHint.split("#")[0]?.trim() ?? "mkcert -install"} className="mono" />
                  </p>
                )}
                <dl className="kv">
                  <dt>{t.cert}</dt>
                  <dd>
                    {tls.exists ? (
                      <>
                        <span className="mono micro">{tls.certFile}</span>
                        <span className="meta"> · {tls.notAfter ? t.certUntil(absolute(tls.notAfter)) : ""}</span>
                        <span className={tls.missing.length ? "tone-warning" : "tone-success"}> · {tls.missing.length ? t.missing(tls.missing.length) : t.covered}</span>
                      </>
                    ) : (
                      <span className="meta">{t.noCert}</span>
                    )}
                  </dd>
                  <dt>{t.traefik}</dt>
                  <dd>
                    <span className="mono micro">{tls.traefikFile}</span> <span className="meta">· {tls.traefikReady ? t.traefikReady : t.traefikMissing}</span>
                  </dd>
                </dl>
                <p className="meta">{t.routersHint}</p>
              </div>
            </Frame>
          )}
        </>
      )}
    </main>
  );
}
