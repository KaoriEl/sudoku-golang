export type Status = "running" | "starting" | "unhealthy" | "restarting" | "paused" | "stopped" | "error";

export type ContainerAction = "stop" | "start" | "restart";
export type BulkAction = "start" | "stop" | "restart";

export interface Network {
  name: string;
  ip: string;
  ipv6: string;
  dnsNames: string[] | null;
}

export interface Port {
  containerPort: number;
  protocol: string;
  hostIp: string;
  hostPort: number;
}

export interface Container {
  id: string;
  name: string;
  project: string;
  service: string;
  composeFile: string;
  managed: boolean;
  image: string;
  state: string;
  health: string;
  status: Status;
  exitCode: number;
  error: string;
  startedAt: string;
  finishedAt: string;
  hostname: string;
  networks: Network[] | null;
  ports: Port[] | null;
  hosts: string[] | null;
  routers: Router[] | null;
  disabled: boolean;
  update: UpdateStatus | null;
}

export interface Router {
  name: string;
  kind: string;
  entryPoints: string[] | null;
  rule: string;
}

export interface ComposeFile {
  path: string;
  rel: string;
  project: string;
  infra: boolean;
  services: string[] | null;
  disabled: string[] | null;
  error: string;
}

export type RunStatus = "running" | "success" | "partial" | "failed";
export type StepStatus = "pending" | "running" | "success" | "failed" | "skipped";

export interface Step {
  composeFile: string;
  rel: string;
  phase: string;
  command: string;
  status: StepStatus;
  output: string;
  error: string;
  startedAt: string | null;
  finishedAt: string | null;
}

export interface Run {
  id: string;
  action: string;
  target: string;
  status: RunStatus;
  startedAt: string;
  finishedAt: string | null;
  steps: Step[] | null;
}

export interface Snapshot {
  root: string;
  composeFiles: ComposeFile[] | null;
  containers: Container[] | null;
  run: Run | null;
  maxWorkers: number;
  disabledPath: string;
  generatedAt: string;
  live: boolean;
}

export interface CommandItem {
  id: string;
  label: string;
  command: string;
}

export interface BuildInfo {
  hasBuild: boolean;
  image: string;
  context: string;
  dockerfile: string;
  target: string;
  args: Record<string, string> | null;
  inline: boolean;
  command: string;
}

export interface ContainerCommands {
  container: string;
  service: string;
  composeFile: string;
  commands: CommandItem[];
  build: BuildInfo | null;
  buildError: string;
}

export interface Consumer {
  container: string;
  router: string;
  kind: string;
}

export interface PortRow {
  index: number;
  raw: string;
  parsed: boolean;
  hostIp: string;
  hostPort: number;
  containerPort: number;
  protocol: string;
  comment: string;
  line: number;
  live: boolean;
  entryPoint: string;
  consumers: Consumer[];
}

export interface EntryPointRow {
  name: string;
  address: string;
  port: number;
  protocol: string;
  line: number;
  published: boolean;
  consumers: Consumer[];
}

export interface PortsView {
  composeFile: string;
  traefikConfig: string;
  traefikConfigFound: boolean;
  service: string;
  container: Container | null;
  ports: PortRow[];
  entryPoints: EntryPointRow[];
  needsApply: boolean;
  applyReasons: string[];
  warnings: string[];
}

export interface AddPortRequest {
  hostIp: string;
  hostPort: number;
  containerPort: number;
  protocol: string;
  comment: string;
  entryPoint: string;
}

export interface AutostartStatus {
  supported: boolean;
  enabled: boolean;
  platform: string;
  path: string;
  executable: string;
  workDir: string;
  logPath: string;
  reason: string;
}

export interface Settings {
  root: string;
  canRestart: boolean;
  dockerPath: string;
  composePath: string;
  maxWorkers: number;
  addr: string;
  autostart: AutostartStatus;
  metrics: { background: boolean; collecting: boolean; liveInterval: number; backgroundInterval: number; prefsPath: string };
  startup: { waitHealthy: boolean; waitTimeout: number };
  notifications: { desktop: boolean; browser: boolean; desktopSupported: boolean; desktopReason: string };
  schedule: {
    cleanup: CleanupSchedule;
    updates: { enabled: boolean; everyHours: number; lastRun: string };
    nextCleanup: string | null;
    nextUpdates: string | null;
    cacheModes: string[];
  };
  backups: { keep: number; dir: string };
  version: VersionInfo;
  auth: { token: boolean; socket: boolean };
  endpoint: string;
}

export interface CleanupSchedule {
  enabled: boolean;
  everyHours: number;
  buildCache: string;
  danglingImages: boolean;
  stoppedContainers: boolean;
  lastRun: string;
  lastResult: string;
}

export interface VersionInfo {
  version: string;
  commit: string;
  date: string;
  go: string;
  os: string;
  arch: string;
  dirty: boolean;
}

export interface PrefsUpdate {
  startup?: { waitHealthy?: boolean; waitTimeout?: number };
  notifications?: { desktop?: boolean; browser?: boolean };
  cleanup?: Partial<Pick<CleanupSchedule, "enabled" | "everyHours" | "buildCache" | "danglingImages" | "stoppedContainers">>;
  updates?: { enabled?: boolean; everyHours?: number };
  backups?: { keep?: number; dir?: string };
}

export interface ToggleResult {
  composeFile: string;
  service: string;
  disabled: boolean;
  dependents: string[];
  storePath: string;
}

export interface Sample {
  t: number;
  cpu: number;
  mem: number;
  memLimit: number;
  netRx: number;
  netTx: number;
  blkR: number;
  blkW: number;
  pids: number;
}

export interface SeriesMeta {
  name: string;
  project: string;
  service: string;
}

export interface MetricsHistory {
  interval: number;
  background: number;
  gapMs: number;
  window: number;
  totals: Sample[] | null;
  containers: Record<string, SeriesMeta & { samples: Sample[] | null }> | null;
}

export interface MetricsTick {
  t: number;
  totals: Sample;
  containers: Record<string, SeriesMeta & { sample: Sample }> | null;
}

export interface EngineInfo {
  ncpu: number;
  memTotal: number;
  serverVersion: string;
  operatingSystem: string;
  kernelVersion: string;
  driver: string;
  cgroupVersion: string;
  containers: number;
  running: number;
  images: number;
  name: string;
}

export interface DiskUsage {
  images: number;
  imagesSize: number;
  imagesUnused: number;
  containers: number;
  containersSize: number;
  volumes: number;
  volumesSize: number;
  buildCache: number;
  buildCacheSize: number;
  buildCacheInUse: number;
}

export interface HostMetrics {
  info: EngineInfo | null;
  infoError: string;
  disk: DiskUsage | null;
  diskError: string;
  diskAt: string | null;
  socket: string;
  lastSample: string | null;
  collectError: string;
  background: boolean;
  liveInterval: number;
  backgroundInterval: number;
}

export interface CleanupContainer {
  id: string;
  name: string;
  image: string;
  project: string;
  service: string;
  state: string;
  status: string;
  size: number;
  created: string;
  protected: boolean;
  reason: "" | "volume" | "database";
  volumes: string[];
}

export interface CleanupImage {
  id: string;
  tags: string[];
  size: number;
  unique: number;
  created: string;
  dangling: boolean;
  project: string;
  service: string;
}

export type CacheMode = "all" | "24h" | "168h" | "720h";

export interface CleanupPreview {
  scannedAt: string;
  containers: CleanupContainer[];
  images: CleanupImage[];
  buildCache: {
    count: number;
    total: number;
    inUse: number;
    reclaimable: number;
    buckets: { mode: CacheMode; size: number; count: number }[];
  };
  volumes: { count: number; size: number; unused: number; unusedSize: number };
}

export interface CleanupRequest {
  containers: string[];
  images: string[];
  buildCache: CacheMode | "";
}

export interface CleanupResult {
  items: { kind: "container" | "image" | "buildCache"; id: string; name: string; size: number; ok: boolean; error: string }[];
  freed: number;
}

export type SelectionAction = "start" | "stop" | "restart" | "disable" | "enable";

export interface SelectionTarget {
  composeFile: string;
  service: string;
}

export interface SelectionResult {
  run: Run | null;
  toggled: ToggleResult[];
  dependents: string[];
  storePath: string;
}

export interface ConfigField {
  key: string;
  value: string;
  inFile: boolean;
  effective: string;
  default: string;
  source: "file" | "env" | "secret" | "default" | "flag";
}

export interface ConfigView {
  path: string;
  exists: boolean;
  fields: ConfigField[];
  otherKeys: string[];
  addr: string;
  addrFromFlag: boolean;
  canRestart: boolean;
}

export interface ConfigSaved {
  config: ConfigView;
  changed: string[];
  restarting: boolean;
  url: string;
}

export interface UpdateStatus {
  image: string;
  local: string;
  remote: string;
  available: boolean;
  localBuild: boolean;
  error: string;
  checkedAt: string;
}

export interface UpdatesView {
  running: boolean;
  checkedAt: string | null;
  images: UpdateStatus[];
  available: number;
}

export type AlertKind = "crash" | "unhealthy" | "oom";

export interface Alert {
  id: string;
  kind: AlertKind;
  container: string;
  project: string;
  service: string;
  exitCode: string;
  at: string;
}

export interface RunSummary {
  id: string;
  action: string;
  target: string;
  status: RunStatus;
  startedAt: string;
  finishedAt: string | null;
  steps: number;
  failed: number;
}

export interface SetView {
  name: string;
  description: string;
  targets: string[];
  resolved: SelectionTarget[];
  files: string[];
  error: string;
}

export interface SetsView {
  path: string;
  sets: SetView[];
}

export interface PortConflict {
  file: string;
  service: string;
  hostIp: string;
  port: number;
  proto: string;
  kind: "container" | "process" | "unknown";
  owner: string;
  containerId?: string;
  pid?: number;
}

export interface PreflightResult {
  conflicts: PortConflict[];
  checked: number;
}

export interface PreflightRequest {
  targets?: SelectionTarget[];
  set?: string;
}

export interface ConfigVar {
  name: string;
  set: boolean;
  default: string;
  required: boolean;
  uses: number;
}

export interface ServiceConfig {
  service: string;
  composeFile: string;
  yaml: string;
  rawYaml: string;
  variables: ConfigVar[];
  warnings: string[];
  error: string;
}

export interface BackupTarget {
  id: string;
  name: string;
  project: string;
  service: string;
  composeFile: string;
  image: string;
  kind: string;
  running: boolean;
  last: string | null;
  count: number;
}

export interface BackupItem {
  id: string;
  container: string;
  project: string;
  service: string;
  kind: string;
  image: string;
  file: string;
  size: number;
  createdAt: string;
}

export interface BackupsView {
  dir: string;
  keep: number;
  targets: BackupTarget[];
  backups: BackupItem[];
}

export interface DomainHost {
  name: string;
  addrs: string[];
  resolves: boolean;
  local: boolean;
  autoLocal: boolean;
  inBlock: boolean;
  inCert: boolean;
  containers: string[];
}

export interface CertStatus {
  mkcert: boolean;
  mkcertPath: string;
  caRoot: string;
  caReady: boolean;
  installHint: string;
  certFile: string;
  exists: boolean;
  hosts: string[];
  missing: string[];
  notAfter: string | null;
  traefikFile: string;
  traefikReady: boolean;
  dir: string;
}

export interface DomainsView {
  hostsFile: string;
  hosts: DomainHost[];
  block: string[];
  pending: string[];
  tls: CertStatus;
  command: string;
  staged: string;
  written: boolean;
}

export type DoctorLevel = "ok" | "info" | "warn" | "fail";

export interface DoctorCheck {
  group: string;
  title: string;
  level: DoctorLevel;
  detail: string;
  hint: string;
}

export interface DoctorReport {
  checks: DoctorCheck[];
  fails: number;
  warns: number;
  version: string;
  at: string;
}

export interface LogLine {
  t: string;
  s: "out" | "err";
  m: string;
  c?: string;
}

export type InfraFileId = "compose" | "traefik";

export interface InfraFile {
  id: InfraFileId;
  path: string;
  exists: boolean;
  content: string;
  hash: string;
  modTime: string;
  hasBackup: boolean;
}

export interface InfraFilesView {
  files: InfraFile[];
}

export interface InfraSaved {
  file: InfraFile;
  run?: Run;
  applyError?: string;
}

export interface SelfRelease {
  tag: string;
  name: string;
  url: string;
  notes: string;
  published: string;
  hasAsset: boolean;
}

export interface SelfUpdateView {
  current: string;
  isRelease: boolean;
  latest?: SelfRelease;
  newer: boolean;
  checkedAt?: string;
  checkError?: string;
  canApply: boolean;
  reason?: string;
  exe: string;
  installed?: string;
}
