import type {
  AddPortRequest,
  BulkAction,
  CleanupPreview,
  ConfigSaved,
  ConfigView,
  SelectionAction,
  SelectionResult,
  SelectionTarget,
  CleanupRequest,
  CleanupResult,
  Container,
  ContainerAction,
  ContainerCommands,
  InfraFileId,
  InfraFilesView,
  InfraSaved,
  SelfUpdateView,
  HostMetrics,
  ToggleResult,
  PortsView,
  Run,
  Settings,
  Snapshot,
  Alert,
  BackupsView,
  DoctorReport,
  DomainsView,
  PreflightRequest,
  PreflightResult,
  PrefsUpdate,
  RunSummary,
  ServiceConfig,
  SetsView,
  UpdatesView,
} from "./types";

export class ApiError extends Error {
  constructor(
    message: string,
    readonly code: string,
    readonly status: number,
  ) {
    super(message);
  }
}

async function request<T>(path: string, method: "GET" | "POST" = "GET", body: unknown = {}): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, {
      method,
      headers: method === "POST" ? { "Content-Type": "application/json" } : undefined,
      body: method === "POST" ? JSON.stringify(body) : undefined,
    });
  } catch {
    throw new ApiError("network error", "network", 0);
  }

  const data: unknown = await res.json().catch(() => null);
  if (!res.ok) {
    const err = data as { error?: string; code?: string } | null;
    throw new ApiError(err?.error ?? res.statusText, err?.code ?? "http", res.status);
  }
  return data as T;
}

const enc = encodeURIComponent;

export const api = {
  state: () => request<Snapshot>("/api/state"),
  run: (id: string) => request<Run>(`/api/runs/${enc(id)}`),
  bulk: (action: BulkAction) => request<Run>(`/api/actions/${action}`, "POST"),
  container: (id: string, action: ContainerAction) => request<Container>(`/api/containers/${enc(id)}/${action}`, "POST"),
  commands: (id: string) => request<ContainerCommands>(`/api/containers/${enc(id)}/commands`),
  rebuild: (id: string, noCache: boolean) => request<Run>(`/api/containers/${enc(id)}/rebuild`, "POST", { noCache }),
  ports: () => request<PortsView>("/api/ports"),
  addPort: (req: AddPortRequest) => request<PortsView>("/api/ports", "POST", req),
  removePort: (index: number, raw: string, removeEntryPoint: boolean) =>
    request<PortsView>("/api/ports/remove", "POST", { index, raw, removeEntryPoint }),
  applyPorts: () => request<Run>("/api/ports/apply", "POST"),
  selfUpdate: () => request<SelfUpdateView>("/api/selfupdate"),
  checkSelfUpdate: () => request<SelfUpdateView>("/api/selfupdate/check", "POST"),
  applySelfUpdate: (force: boolean) => request<SelfUpdateView>("/api/selfupdate/apply", "POST", { force }),
  restartAdmin: () => request<{ restarting: boolean }>("/api/restart", "POST"),
  infraFiles: () => request<InfraFilesView>("/api/infra/files"),
  saveInfraFile: (id: InfraFileId, body: { content: string; hash: string; apply: boolean }) =>
    request<InfraSaved>(`/api/infra/files/${id}`, "POST", body),
  settings: () => request<Settings>("/api/settings"),
  setAutostart: (enabled: boolean) => request<Settings>("/api/settings/autostart", "POST", { enabled }),
  setMetrics: (update: { background?: boolean; liveInterval?: number; backgroundInterval?: number }) =>
    request<Settings>("/api/settings/metrics", "POST", update),
  setDisabled: (composeFile: string, service: string, disabled: boolean) =>
    request<ToggleResult>("/api/services/disabled", "POST", { composeFile, service, disabled }),
  selection: (action: SelectionAction, targets: SelectionTarget[]) => request<SelectionResult>("/api/selection", "POST", { action, targets }),
  config: () => request<ConfigView>("/api/config"),
  saveConfig: (values: Record<string, string>) => request<ConfigSaved>("/api/config", "POST", { values }),
  cleanupPreview: () => request<CleanupPreview>("/api/cleanup"),
  cleanupRun: (req: CleanupRequest) => request<CleanupResult>("/api/cleanup", "POST", req),
  metricsHost: (refresh = false) => request<HostMetrics>(`/api/metrics/host${refresh ? "?refresh=1" : ""}`),
  runs: () => request<RunSummary[]>("/api/runs"),
  alerts: () => request<Alert[]>("/api/alerts"),
  sets: () => request<SetsView>("/api/sets"),
  saveSet: (name: string, description: string, targets: string[]) => request<SetsView>("/api/sets", "POST", { name, description, targets }),
  deleteSet: (name: string) => request<SetsView>("/api/sets/delete", "POST", { name }),
  runSet: (name: string, action: BulkAction) => request<Run>(`/api/sets/${enc(name)}/${action}`, "POST"),
  preflight: (req: PreflightRequest) => request<PreflightResult>("/api/preflight", "POST", req),
  serviceConfig: (id: string) => request<ServiceConfig>(`/api/containers/${enc(id)}/config`),
  updates: () => request<UpdatesView>("/api/updates"),
  checkUpdates: () => request<UpdatesView>("/api/updates/check", "POST"),
  backups: () => request<BackupsView>("/api/backups"),
  createBackups: (containers: string[]) => request<Run>("/api/backups", "POST", { containers }),
  restoreBackup: (id: string, container = "") => request<Run>("/api/backups/restore", "POST", { id, container }),
  deleteBackup: (id: string) => request<{ id: string }>("/api/backups/delete", "POST", { id }),
  domains: () => request<DomainsView>("/api/domains"),
  writeHosts: () => request<DomainsView>("/api/domains/hosts", "POST"),
  issueCert: () => request<{ domains: DomainsView; output: string }>("/api/domains/cert", "POST"),
  doctor: () => request<DoctorReport>("/api/doctor"),
  setPrefs: (update: PrefsUpdate) => request<Settings>("/api/settings/prefs", "POST", update),
};

export const EVENTS_STREAM = "/api/events";

export function logsDownloadUrl(id: string, all: boolean): string {
  return `/api/containers/${enc(id)}/logs/download${all ? "" : "?tail=10000"}`;
}

export function projectLogsUrl(file: string, services: string[], tail: number): string {
  const q = new URLSearchParams({ file, tail: String(tail) });
  if (services.length > 0) q.set("services", services.join(","));
  return `/api/logs/project?${q.toString()}`;
}

export function backupDownloadUrl(id: string): string {
  return `/api/backups/download?id=${enc(id)}`;
}

export const METRICS_STREAM = "/api/metrics/stream";

export function logsUrl(id: string, tail: number): string {
  return `/api/containers/${enc(id)}/logs?tail=${tail}`;
}

export function execUrl(id: string, cols: number, rows: number): string {
  const proto = window.location.protocol === "https:" ? "wss" : "ws";
  return `${proto}://${window.location.host}/api/containers/${enc(id)}/exec?cols=${cols}&rows=${rows}`;
}
