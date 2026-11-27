import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, api } from "../../api/client";
import type { Settings } from "../../api/types";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { Button } from "../../components/ui/Button";
import { Switch } from "../../components/ui/Checkbox";
import { CopyText } from "../../components/ui/CopyButton";
import { Frame, SectionBar } from "../../components/ui/Frame";
import { useToast } from "../../components/ui/Toasts";
import { ru } from "../../i18n/ru";
import { usePref } from "../../lib/hooks";
import { ConfigSection } from "./ConfigSection";
import { PrefsSections } from "./PrefsSections";

const t = ru.settingsPage;

const LIVE_OPTIONS = [1, 2, 3, 5, 10, 15, 30, 60];
const BG_OPTIONS = [5, 10, 15, 30, 60, 120, 300, 600, 1800, 3600];

function IntervalField({
  label,
  hint,
  value,
  options,
  disabled,
  onChange,
}: {
  label: string;
  hint: string;
  value: number;
  options: number[];
  disabled: boolean;
  onChange: (v: number) => void;
}) {
  const all = options.includes(value) ? options : [...options, value].sort((a, b) => a - b);
  return (
    <label className={`field ${disabled ? "is-disabled" : ""}`}>
      <span className="field-label">{label}</span>
      <span className="input select">
        <select value={value} disabled={disabled} onChange={(e) => onChange(Number(e.target.value))}>
          {all.map((o) => (
            <option key={o} value={o}>
              {ru.settingsPage.every(o)}
            </option>
          ))}
        </select>
      </span>
      <span className="field-help">{hint}</span>
    </label>
  );
}

function ConfirmPref({ prefKey }: { prefKey: string }) {
  const [skipped, setSkipped] = usePref(prefKey);
  const name = t.confirmNames[prefKey.replace("confirm.", "")] ?? prefKey;
  return (
    <div className="pref-row">
      <span>{name}</span>
      <span className={`badge badge-${skipped ? "warning" : "success"}`}>
        <StatusGlyph kind={skipped ? "triangle" : "square"} size={10} />
        {skipped ? t.skipped : t.asked}
      </span>
      <Button size="sm" variant="outline" disabled={!skipped} onClick={() => setSkipped(false)}>
        {t.askAgain}
      </Button>
    </div>
  );
}

export function SettingsPage() {
  const qc = useQueryClient();
  const toast = useToast();
  const q = useQuery({ queryKey: ["settings"], queryFn: api.settings, refetchInterval: 10_000 });
  const toggle = useMutation({
    mutationFn: api.setAutostart,
    onSuccess: (s: Settings) => qc.setQueryData(["settings"], s),
    onError: (e) => toast({ tone: "danger", message: t.autostart, detail: e instanceof ApiError ? e.message : String(e) }),
  });

  const bg = useMutation({
    mutationFn: api.setMetrics,
    onSuccess: (st: Settings) => qc.setQueryData(["settings"], st),
    onError: (e) => toast({ tone: "danger", message: t.metrics, detail: e instanceof ApiError ? e.message : String(e) }),
  });

  const s = q.data;
  const a = s?.autostart;

  return (
    <main className="page" id="main">
      <header className="page-head">
        <SectionBar title={t.title} jp={t.titleJp} level={1} />
      </header>

      <Frame label={t.autostart}>
        <div className="settings-block">
          <SectionBar title={t.autostart} />
          {a ? (
            <>
              <Switch checked={a.enabled} disabled={(!a.supported && !a.enabled) || toggle.isPending} onChange={(v) => toggle.mutate(v)}>
                {t.autostartLabel}
              </Switch>
              <p className="meta">{a.reason ? `${t.unsupported}: ${a.reason}` : a.enabled ? t.autostartOn : t.autostartOff}</p>
              <dl className="kv">
                <dt>{t.binary}</dt>
                <dd className="mono">{a.executable || "—"}</dd>
                <dt>{t.workDir}</dt>
                <dd className="mono">{a.workDir || "—"}</dd>
                <dt>{t.autostartFile}</dt>
                <dd>{a.path ? <CopyText value={a.path} className="mono" /> : "—"}</dd>
                <dt>{t.logPath}</dt>
                <dd>{a.logPath ? <CopyText value={a.logPath} className="mono" /> : "—"}</dd>
              </dl>
            </>
          ) : (
            <div className="skeleton-row" />
          )}
        </div>
      </Frame>

      <Frame label={t.metrics}>
        <div className="settings-block">
          <SectionBar title={t.metrics} />
          {s ? (
            <>
              <div className="form-grid form-grid-narrow">
                <IntervalField
                  label={t.liveInterval}
                  hint={t.liveHint}
                  value={s.metrics.liveInterval}
                  options={LIVE_OPTIONS}
                  disabled={bg.isPending}
                  onChange={(v) => bg.mutate({ liveInterval: v })}
                />
              </div>
              <Switch checked={s.metrics.background} disabled={bg.isPending} onChange={(v) => bg.mutate({ background: v })}>
                {t.metricsLabel}
              </Switch>
              <div className="form-grid form-grid-narrow">
                <IntervalField
                  label={t.bgInterval}
                  hint={t.bgHint}
                  value={s.metrics.backgroundInterval}
                  options={BG_OPTIONS}
                  disabled={bg.isPending || !s.metrics.background}
                  onChange={(v) => bg.mutate({ backgroundInterval: v })}
                />
              </div>
              <p className="meta">{s.metrics.background ? t.metricsOn(t.every(s.metrics.backgroundInterval)) : t.metricsOff}</p>
              <p className="meta micro">
                <span className={s.metrics.collecting ? "tone-success" : ""}>{t.metricsNow(s.metrics.collecting)}</span> · {t.metricsFile}{" "}
                <span className="mono">{s.metrics.prefsPath}</span>
              </p>
            </>
          ) : (
            <div className="skeleton-row" />
          )}
        </div>
      </Frame>

      <Frame label={t.confirmations}>
        <div className="settings-block">
          <SectionBar title={t.confirmations} />
          <p className="meta">{t.confirmHint}</p>
          <ConfirmPref prefKey="confirm.stopAll" />
          <ConfirmPref prefKey="confirm.restartAll" />
          <ConfirmPref prefKey="confirm.applyPorts" />
        </div>
      </Frame>

      {s && <PrefsSections s={s} />}
      <ConfigSection />
    </main>
  );
}
