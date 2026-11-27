import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Bell, Stethoscope } from "lucide-react";
import { useState } from "react";
import { ApiError, api } from "../../api/client";
import type { DoctorReport, PrefsUpdate, Settings } from "../../api/types";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { Button } from "../../components/ui/Button";
import { Checkbox, Switch } from "../../components/ui/Checkbox";
import { CopyText } from "../../components/ui/CopyButton";
import { Frame, SectionBar } from "../../components/ui/Frame";
import { useToast } from "../../components/ui/Toasts";
import { rf } from "../../i18n/ru.features";
import { absolute } from "../../lib/format";

const t = rf.prefs;
const TIMEOUTS = [30, 60, 120, 180, 300, 600, 900, 1800];
const HOURS = [1, 6, 12, 24, 48, 72, 168, 336, 720];
const KEEP = [3, 5, 10, 20, 30, 50, 100];
const iconProps = { size: 14, strokeWidth: 2, strokeLinecap: "square" as const };
const levelGlyph = { ok: "square", info: "outline", warn: "triangle", fail: "fault" } as const;
const levelTone = { ok: "success", info: "neutral", warn: "warning", fail: "danger" } as const;

function Select<T extends string | number>({
  label,
  value,
  options,
  format,
  disabled,
  onChange,
}: {
  label: string;
  value: T;
  options: T[];
  format: (v: T) => string;
  disabled?: boolean;
  onChange: (v: T) => void;
}) {
  const all = options.includes(value) ? options : [...options, value];
  return (
    <label className={`field ${disabled ? "is-disabled" : ""}`}>
      <span className="field-label">{label}</span>
      <span className="input select">
        <select
          value={String(value)}
          disabled={disabled}
          onChange={(e) => onChange((typeof value === "number" ? Number(e.target.value) : e.target.value) as T)}
        >
          {all.map((o) => (
            <option key={String(o)} value={String(o)}>
              {format(o)}
            </option>
          ))}
        </select>
      </span>
    </label>
  );
}

function Doctor() {
  const [report, setReport] = useState<DoctorReport | null>(null);
  const run = useMutation({ mutationFn: api.doctor, onSuccess: setReport });
  let group = "";
  return (
    <div className="settings-block">
      <SectionBar title={t.doctor}>
        <Button variant="outline" icon={<Stethoscope {...iconProps} />} loading={run.isPending} loadingLabel={t.doctorRunning} onClick={() => run.mutate()}>
          {t.doctorRun}
        </Button>
      </SectionBar>
      <p className="meta">{t.doctorCli}</p>
      {run.isError && <p className="tone-danger mono">{run.error instanceof ApiError ? run.error.message : String(run.error)}</p>}
      {report && (
        <>
          <p className={report.fails ? "tone-danger" : report.warns ? "tone-warning" : "tone-success"}>{t.doctorSummary(report.fails, report.warns)}</p>
          <ul className="doctor-list">
            {report.checks.map((c, i) => {
              const head = c.group !== group;
              group = c.group;
              return (
                <li key={`${c.group}-${c.title}-${i}`} className={`doctor-item tone-${levelTone[c.level]}`}>
                  {head && <div className="doctor-group">{c.group}</div>}
                  <div className="doctor-row">
                    <StatusGlyph kind={levelGlyph[c.level]} size={10} label={t.levels[c.level]} />
                    <span className="doctor-title">{c.title}</span>
                    <span className="doctor-detail mono micro">{c.detail}</span>
                  </div>
                  {c.hint && c.level !== "ok" && <div className="doctor-hint meta">{c.hint}</div>}
                </li>
              );
            })}
          </ul>
        </>
      )}
    </div>
  );
}

function permission(): NotificationPermission | "unsupported" {
  return typeof Notification === "undefined" ? "unsupported" : Notification.permission;
}

export function PrefsSections({ s }: { s: Settings }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [perm, setPerm] = useState(permission);
  const save = useMutation({
    mutationFn: (u: PrefsUpdate) => api.setPrefs(u),
    onSuccess: (st: Settings) => qc.setQueryData(["settings"], st),
    onError: (e) => toast({ tone: "danger", message: t.saved, detail: e instanceof ApiError ? e.message : String(e) }),
  });
  const set = (u: PrefsUpdate) => save.mutate(u);
  const c = s.schedule.cleanup;
  const u = s.schedule.updates;
  const busy = save.isPending;

  return (
    <>
      <Frame label={t.startup}>
        <div className="settings-block">
          <SectionBar title={t.startup} />
          <Switch checked={s.startup.waitHealthy} disabled={busy} onChange={(v) => set({ startup: { waitHealthy: v } })}>
            {t.waitHealthy}
          </Switch>
          <Select label={t.waitTimeout} value={s.startup.waitTimeout} options={TIMEOUTS} format={t.minutes} disabled={busy || !s.startup.waitHealthy} onChange={(v) => set({ startup: { waitTimeout: v } })} />
          <p className="meta">{t.waitHint}</p>
        </div>
      </Frame>

      <Frame label={t.notifications}>
        <div className="settings-block">
          <SectionBar title={t.notifications} />
          <Switch checked={s.notifications.desktop} disabled={busy || (!s.notifications.desktopSupported && !s.notifications.desktop)} onChange={(v) => set({ notifications: { desktop: v } })}>
            {t.desktop}
          </Switch>
          {!s.notifications.desktopSupported && <p className="meta tone-warning">{t.desktopUnsupported(s.notifications.desktopReason)}</p>}
          <Switch checked={s.notifications.browser} disabled={busy} onChange={(v) => set({ notifications: { browser: v } })}>
            {t.browser}
          </Switch>
          {s.notifications.browser && perm === "default" && (
            <Button size="sm" variant="outline" icon={<Bell {...iconProps} />} onClick={() => void Notification.requestPermission().then(setPerm)}>
              {t.browserPermission}
            </Button>
          )}
          {s.notifications.browser && perm === "denied" && <p className="meta tone-warning">{t.browserDenied}</p>}
          <p className="meta">{t.notifyHint}</p>
        </div>
      </Frame>

      <Frame label={t.schedule}>
        <div className="settings-block">
          <SectionBar title={t.cleanup} />
          <Switch checked={c.enabled} disabled={busy} onChange={(v) => set({ cleanup: { enabled: v } })}>
            {t.cleanupEnabled}
          </Switch>
          <div className="form-grid">
            <Select label={t.every} value={c.everyHours} options={HOURS} format={t.everyH} disabled={busy || !c.enabled} onChange={(v) => set({ cleanup: { everyHours: v } })} />
            <Select
              label={t.buildCache}
              value={c.buildCache}
              options={["off", ...s.schedule.cacheModes]}
              format={(m) => t.cacheModes[m] ?? m}
              disabled={busy || !c.enabled}
              onChange={(v) => set({ cleanup: { buildCache: v } })}
            />
          </div>
          <Checkbox checked={c.danglingImages} onChange={(v) => set({ cleanup: { danglingImages: v } })}>
            {t.dangling}
          </Checkbox>
          <Checkbox checked={c.stoppedContainers} onChange={(v) => set({ cleanup: { stoppedContainers: v } })}>
            {t.stopped}
          </Checkbox>
          <p className="meta">
            {c.lastRun && !c.lastRun.startsWith("0001") ? t.lastRun(absolute(c.lastRun), c.lastResult) : t.never}
            {s.schedule.nextCleanup && ` · ${t.nextRun(absolute(s.schedule.nextCleanup))}`}
          </p>

          <SectionBar title={t.updates} />
          <Switch checked={u.enabled} disabled={busy} onChange={(v) => set({ updates: { enabled: v } })}>
            {t.updatesEnabled}
          </Switch>
          <Select label={t.every} value={u.everyHours} options={HOURS} format={t.everyH} disabled={busy || !u.enabled} onChange={(v) => set({ updates: { everyHours: v } })} />
          <p className="meta">
            {u.lastRun && !u.lastRun.startsWith("0001") ? absolute(u.lastRun) : t.never}
            {s.schedule.nextUpdates && ` · ${t.nextRun(absolute(s.schedule.nextUpdates))}`}
          </p>
        </div>
      </Frame>

      <Frame label={t.backups}>
        <div className="settings-block">
          <SectionBar title={t.backups} />
          <Select label={t.keep} value={s.backups.keep} options={KEEP} format={t.keepN} disabled={busy} onChange={(v) => set({ backups: { keep: v } })} />
          <dl className="kv">
            <dt>{t.dir}</dt>
            <dd>
              <CopyText value={s.backups.dir} className="mono" />
            </dd>
          </dl>
        </div>
      </Frame>

      <Frame label={t.version}>
        <div className="settings-block">
          <SectionBar title={t.version} />
          <dl className="kv">
            <dt>{t.version}</dt>
            <dd className="mono">
              {s.version.version}
              {s.version.commit && ` (${s.version.commit}${s.version.dirty ? "-dirty" : ""})`} · {s.version.os}/{s.version.arch} · {s.version.go}
            </dd>
            <dt>{t.endpoint}</dt>
            <dd className="mono">{s.endpoint}</dd>
            <dt>{t.auth}</dt>
            <dd>
              {s.auth.token ? (
                <span className="tone-success inline-status">
                  <StatusGlyph kind="square" size={10} /> {t.tokenOn}
                </span>
              ) : (
                <span className="meta">{t.tokenOff}</span>
              )}
              {s.auth.socket && <span className="meta"> · {t.socket}</span>}
            </dd>
          </dl>
          <p className="meta">
            {t.updateHint} <CopyText value="sudoku update" className="mono" />
          </p>
        </div>
      </Frame>

      <Frame label={t.doctor}>
        <Doctor />
      </Frame>
    </>
  );
}
