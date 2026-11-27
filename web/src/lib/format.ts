import { ru } from "../i18n/ru";
import type { Port, Status } from "../api/types";

export function parseTime(value: string | null | undefined): number | null {
  if (!value || value.startsWith("0001-")) return null;
  const t = Date.parse(value);
  return Number.isNaN(t) ? null : t;
}

export function ago(value: string | number | null | undefined, now: number): string {
  const t = typeof value === "number" ? value : parseTime(value);
  if (t === null) return "";
  const s = Math.max(0, Math.round((now - t) / 1000));
  if (s < 5) return ru.time.justNow;
  if (s < 60) return ru.time.seconds(s);
  const m = Math.floor(s / 60);
  if (m < 60) return ru.time.minutes(m);
  const h = Math.floor(m / 60);
  if (h < 48) return ru.time.hours(h);
  return ru.time.days(Math.floor(h / 24));
}

export function duration(from: string | null, to: string | null, now: number): string {
  const a = parseTime(from);
  if (a === null) return "";
  const b = parseTime(to) ?? now;
  const s = Math.max(0, Math.round((b - a) / 1000));
  const mm = String(Math.floor(s / 60)).padStart(2, "0");
  const ss = String(s % 60).padStart(2, "0");
  return `${mm}:${ss}`;
}

export function clock(now: number): string {
  return new Intl.DateTimeFormat("ru-RU", { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(now);
}

export function absolute(value: string | null | undefined): string {
  const t = parseTime(value);
  if (t === null) return "";
  return new Intl.DateTimeFormat("ru-RU", { dateStyle: "short", timeStyle: "medium" }).format(t);
}

export function hostLabel(hostIp: string): string {
  if (!hostIp || hostIp === "0.0.0.0" || hostIp === "::" || hostIp === "127.0.0.1" || hostIp === "::1") return "localhost";
  return hostIp;
}

export function portUrl(p: Port): string | null {
  if (p.protocol !== "tcp" || p.hostPort === 0) return null;
  const scheme = p.containerPort === 443 || p.hostPort === 443 ? "https" : "http";
  return `${scheme}://${hostLabel(p.hostIp)}:${p.hostPort}`;
}

const hexId = /^[0-9a-f]{12}$/;

export function dnsAliases(names: string[] | null, containerName: string): string[] {
  return (names ?? []).filter((n) => !hexId.test(n) && n !== containerName);
}

export type Tone = "success" | "warning" | "danger" | "progress" | "neutral";

export const statusTone: Record<Status, Tone> = {
  running: "success",
  starting: "warning",
  unhealthy: "warning",
  restarting: "warning",
  paused: "neutral",
  stopped: "neutral",
  error: "danger",
};

export function isUp(status: Status): boolean {
  return status === "running" || status === "starting" || status === "unhealthy" || status === "restarting";
}

export function isFault(status: Status): boolean {
  return status === "error" || status === "unhealthy" || status === "restarting";
}
