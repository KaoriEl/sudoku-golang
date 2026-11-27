import { useEffect, useSyncExternalStore } from "react";
import { METRICS_STREAM } from "../api/client";
import type { MetricsHistory, MetricsTick, Sample, SeriesMeta } from "../api/types";

export const WINDOW_MS = 30 * 60 * 1000;
const HIDDEN_GRACE_MS = 30_000;

export interface ContainerSeries extends SeriesMeta {
  samples: Sample[];
}

export type StreamState = "connecting" | "live" | "error";

class MetricsStore {
  totals: Sample[] = [];
  containers = new Map<string, ContainerSeries>();
  interval = 2000;
  gapMs = 25_000;
  state: StreamState = "connecting";
  version = 0;
  private listeners = new Set<() => void>();
  private es: EventSource | null = null;
  private refs = 0;
  private retry: number | null = null;
  private hideTimer: number | null = null;
  private visibilityBound = false;

  subscribe = (fn: () => void) => {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  };

  getVersion = () => this.version;

  private emit() {
    this.version++;
    this.listeners.forEach((fn) => fn());
  }

  acquire() {
    this.refs++;
    this.bindVisibility();
    if (this.refs === 1 && !document.hidden) this.open();
  }

  release() {
    this.refs = Math.max(0, this.refs - 1);
    if (this.refs === 0) this.close();
  }

  private bindVisibility() {
    if (this.visibilityBound) return;
    this.visibilityBound = true;
    document.addEventListener("visibilitychange", () => {
      if (document.hidden) {
        if (this.hideTimer === null && this.es) {
          this.hideTimer = window.setTimeout(() => {
            this.hideTimer = null;
            if (document.hidden) this.close();
          }, HIDDEN_GRACE_MS);
        }
        return;
      }
      if (this.hideTimer !== null) {
        window.clearTimeout(this.hideTimer);
        this.hideTimer = null;
      }
      if (this.refs > 0 && !this.es && this.retry === null) this.open();
    });
  }

  private open() {
    this.state = "connecting";
    this.emit();
    const es = new EventSource(METRICS_STREAM);
    this.es = es;
    es.addEventListener("history", (e) => {
      const h = JSON.parse((e as MessageEvent<string>).data) as MetricsHistory;
      this.interval = h.interval;
      this.gapMs = h.gapMs || 25_000;
      this.totals = h.totals ?? [];
      this.containers = new Map(Object.entries(h.containers ?? {}).map(([id, s]) => [id, { ...s, samples: s.samples ?? [] }]));
      this.state = "live";
      this.emit();
    });
    es.addEventListener("tick", (e) => {
      const tick = JSON.parse((e as MessageEvent<string>).data) as MetricsTick;
      const cutoff = tick.t - WINDOW_MS;
      this.totals = trim([...this.totals, tick.totals], cutoff);
      for (const [id, c] of Object.entries(tick.containers ?? {})) {
        const prev = this.containers.get(id);
        this.containers.set(id, {
          name: c.name,
          project: c.project,
          service: c.service,
          samples: trim([...(prev?.samples ?? []), c.sample], cutoff),
        });
      }
      for (const [id, s] of this.containers) {
        s.samples = trim(s.samples, cutoff);
        if (s.samples.length === 0) this.containers.delete(id);
      }
      this.state = "live";
      this.emit();
    });
    es.onerror = () => {
      this.state = "error";
      this.emit();
      es.close();
      this.es = null;
      if (this.refs > 0 && this.retry === null) {
        this.retry = window.setTimeout(() => {
          this.retry = null;
          if (this.refs > 0 && !document.hidden) this.open();
        }, 3000);
      }
    };
  }

  private close() {
    this.es?.close();
    this.es = null;
    if (this.hideTimer !== null) window.clearTimeout(this.hideTimer);
    this.hideTimer = null;
    if (this.retry !== null) window.clearTimeout(this.retry);
    this.retry = null;
  }
}

function trim(samples: Sample[], cutoff: number): Sample[] {
  let i = 0;
  while (i < samples.length && samples[i]!.t < cutoff) i++;
  return i === 0 ? samples : samples.slice(i);
}

export const metricsStore = new MetricsStore();

export function useMetrics(): MetricsStore {
  useEffect(() => {
    metricsStore.acquire();
    return () => metricsStore.release();
  }, []);
  useSyncExternalStore(metricsStore.subscribe, metricsStore.getVersion);
  return metricsStore;
}

export function latest(samples: Sample[]): Sample | undefined {
  return samples[samples.length - 1];
}

export function since(samples: Sample[], from: number): Sample[] {
  let i = 0;
  while (i < samples.length && samples[i]!.t < from) i++;
  return i === 0 ? samples : samples.slice(i);
}

const KIB = 1024;
const units = ["Б", "КиБ", "МиБ", "ГиБ", "ТиБ"];

export function bytes(v: number, digits = 1): string {
  if (!Number.isFinite(v) || v <= 0) return "0 Б";
  const i = Math.min(units.length - 1, Math.floor(Math.log(v) / Math.log(KIB)));
  const n = v / KIB ** i;
  return `${n.toFixed(i === 0 ? 0 : n >= 100 ? 0 : digits)} ${units[i]}`;
}

export function rate(v: number): string {
  return `${bytes(v)}/с`;
}

export function pct(v: number): string {
  if (!Number.isFinite(v)) return "—";
  if (v > 0 && v < 0.1) return "<0.1%";
  return `${v.toFixed(v >= 100 ? 0 : 1)}%`;
}
