import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Activity, Boxes, Command, DatabaseBackup, Eraser, FileCog, History, Moon, Network, Play, RotateCw, ScrollText, Settings2, Square, Sun } from "lucide-react";
import { ApiError, api } from "../api/client";
import type { Alert, BulkAction, Container, ContainerAction, Run, SelectionAction, SelectionTarget, Snapshot } from "../api/types";
import { ProductMark } from "../components/hud/ProductMark";
import { StatusGlyph } from "../components/hud/StatusGlyph";
import { Button } from "../components/ui/Button";
import { CommandPalette, type PaletteItem } from "../components/ui/CommandPalette";
import { ConfirmDialog } from "../components/ui/ConfirmDialog";
import { useToast } from "../components/ui/Toasts";
import { ContainerDrawer, type ContainerTab } from "../features/containers/ContainerDrawer";
import { ContainersPage } from "../features/containers/ContainersPage";
import { CleanupPage } from "../features/cleanup/CleanupPage";
import { MonitoringPage } from "../features/monitoring/MonitoringPage";
import { PortsPage } from "../features/ports/PortsPage";
import { BackupsPage } from "../features/backups/BackupsPage";
import { DomainsPage } from "../features/network/DomainsPage";
import { InfraPage } from "../features/infra/InfraPage";
import { useRestartAdmin } from "../features/settings/useRestartAdmin";
import { usePreflight } from "../features/preflight/Preflight";
import { RunsPage } from "../features/runs/RunsPage";
import { SettingsPage } from "../features/settings/SettingsPage";
import { RunDrawer } from "../features/containers/RunDrawer";
import { ru } from "../i18n/ru";
import { rf, runActionLabel } from "../i18n/ru.features";
import { notifyBrowser, useServerEvents } from "../lib/events";
import { clock, isFault } from "../lib/format";
import { useNow, useRoute, useTheme, type Route } from "../lib/hooks";

const iconProps = { size: 16, strokeWidth: 2, strokeLinecap: "square" as const };

function asBulk(a: string | undefined): BulkAction | null {
  return a === "start" || a === "stop" || a === "restart" ? a : null;
}

type Drawer = { kind: "run" } | { kind: "container"; id: string; tab: ContainerTab } | null;

function errorText(e: unknown): string {
  return e instanceof ApiError ? e.message : String(e);
}

function Clock() {
  const now = useNow(1000);
  return (
    <span className="clock mono" aria-hidden>
      {clock(now)}
    </span>
  );
}

export function App() {
  const qc = useQueryClient();
  const toast = useToast();
  const [theme, toggleTheme] = useTheme();
  const [runId, setRunId] = useState<string | null>(null);
  const [drawer, setDrawer] = useState<Drawer>(null);
  const [route, go, href] = useRoute();
  const setDrawerOpen = (open: boolean) => setDrawer(open ? { kind: "run" } : null);
  const [pending, setPending] = useState<Record<string, ContainerAction>>({});
  const lastRunStatus = useRef<string | null>(null);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [paletteConfirm, setPaletteConfirm] = useState<BulkAction | null>(null);
  const [guard, preflightDialog] = usePreflight();
  const settings = useQuery({ queryKey: ["settings"], queryFn: api.settings, staleTime: 60_000 });
  const restartAdmin = useRestartAdmin();
  const browserNotify = settings.data?.notifications.browser ?? true;

  const onAlert = useCallback(
    (a: Alert) => {
      const title = rf.alerts.toast[a.kind] ?? a.kind;
      const detail = [a.container, a.exitCode && a.kind === "crash" ? rf.alerts.exit(a.exitCode) : ""].filter(Boolean).join(" · ");
      toast({ tone: "danger", message: title, detail });
      if (browserNotify) notifyBrowser(`sudoku · ${title}`, detail);
    },
    [toast, browserNotify],
  );
  const events = useServerEvents(onAlert);
  const pushed = events === "live";

  const state = useQuery({
    queryKey: ["state"],
    queryFn: api.state,
    refetchInterval: (q) => (pushed && q.state.data?.live ? 30_000 : q.state.data?.run?.status === "running" ? 2000 : 3000),
    retry: false,
  });

  const snapshot = state.data;
  const latestRunId = snapshot?.run?.id ?? null;
  const currentRunId = runId ?? latestRunId;

  const run = useQuery({
    queryKey: ["run", currentRunId],
    queryFn: () => api.run(currentRunId as string),
    enabled: currentRunId !== null,
    refetchInterval: (q) => (q.state.data?.status === "running" ? (pushed ? 10_000 : 2000) : false),
  });

  const activeRun: Run | null = run.data ?? null;
  const bulkBusy = activeRun?.status === "running";

  useEffect(() => {
    if (!activeRun) return;
    const prev = lastRunStatus.current;
    lastRunStatus.current = activeRun.status;
    if (prev === "running" && activeRun.status !== "running") {
      void qc.invalidateQueries({ queryKey: ["state"] });
      void qc.invalidateQueries({ queryKey: ["ports"] });
      const steps = activeRun.steps ?? [];
      const ok = steps.filter((s) => s.status === "success").length;
      toast({
        tone: activeRun.status === "success" ? "success" : "danger",
        message: `${runActionLabel(activeRun.action)}: ${ru.run.status[activeRun.status]}`,
        detail: ru.run.summary(ok, steps.length),
        action: { label: ru.toast.details, onClick: () => setDrawerOpen(true) },
      });
    }
  }, [activeRun, qc, toast]);

  const onRunStarted = (r: Run) => {
    lastRunStatus.current = r.status;
    void qc.invalidateQueries({ queryKey: ["runs"] });
    qc.setQueryData(["run", r.id], r);
    setRunId(r.id);
    setDrawer({ kind: "run" });
    toast({ tone: "progress", message: [runActionLabel(r.action), r.target].filter(Boolean).join(" · ") });
  };

  const bulk = useMutation({
    mutationFn: api.bulk,
    onSuccess: onRunStarted,
    onError: (e) => toast({ tone: "danger", message: ru.bulk.busy, detail: errorText(e) }),
  });

  const rebuild = useMutation({
    mutationFn: ({ c, noCache }: { c: Container; noCache: boolean }) => api.rebuild(c.id, noCache),
    onSuccess: onRunStarted,
    onError: (e, { c }) => toast({ tone: "danger", message: ru.actions.failed(c.name), detail: errorText(e) }),
  });

  const toggle = useMutation({
    mutationFn: ({ file, service, disabled }: { file: string; service: string; disabled: boolean }) => api.setDisabled(file, service, disabled),
    onSuccess: (res) => {
      void qc.invalidateQueries({ queryKey: ["state"] });
      toast({
        tone: "success",
        message: res.disabled ? ru.disabled.doneOff(res.service) : ru.disabled.doneOn(res.service),
        detail: res.dependents.length > 0 ? ru.disabled.deps(res.service, res.dependents.join(", ")) : `${ru.disabled.file} ${res.storePath}`,
      });
    },
    onError: (e, { service }) => toast({ tone: "danger", message: ru.actions.failed(service), detail: errorText(e) }),
  });

  const selection = useMutation({
    mutationFn: ({ action, targets }: { action: SelectionAction; targets: SelectionTarget[]; done: () => void }) => api.selection(action, targets),
    onSuccess: (res, { action, done }) => {
      done();
      if (res.run) {
        onRunStarted(res.run);
        return;
      }
      void qc.invalidateQueries({ queryKey: ["state"] });
      const n = res.toggled.length;
      toast({
        tone: "success",
        message: action === "disable" ? ru.selection.doneOff(n) : ru.selection.doneOn(n),
        detail: res.dependents.length > 0 ? ru.selection.deps(res.dependents.join(", ")) : `${ru.disabled.file} ${res.storePath}`,
      });
    },
    onError: (e) => toast({ tone: "danger", message: ru.selection.failed, detail: errorText(e) }),
  });

  const containerAction = useMutation({
    mutationFn: ({ c, action }: { c: Container; action: ContainerAction }) => api.container(c.id, action),
    onMutate: ({ c, action }) => setPending((p) => ({ ...p, [c.id]: action })),
    onSuccess: (updated, { c, action }) => {
      qc.setQueryData<Snapshot>(["state"], (old) =>
        old ? { ...old, containers: (old.containers ?? []).map((x) => (x.id === updated.id ? updated : x)) } : old,
      );
      toast({ tone: "success", message: ru.actions.done[action](c.name) });
    },
    onError: (e, { c }) => toast({ tone: "danger", message: ru.actions.failed(c.name), detail: errorText(e) }),
    onSettled: (_d, _e, { c }) => {
      setPending((p) => {
        const nextPending = { ...p };
        delete nextPending[c.id];
        return nextPending;
      });
      void qc.invalidateQueries({ queryKey: ["state"] });
    },
  });

  const managed = (snapshot?.containers ?? []).filter((c) => c.managed);
  const faults = managed.filter((c) => c.status === "error").length;
  const offline = state.isError;

  useEffect(() => {
    document.documentElement.dataset.alert = faults > 0 ? "true" : "false";
    document.title = faults > 0 ? `▲ ${faults} · ${ru.appTitle}` : ru.appTitle;
  }, [faults]);

  const telegraph = offline
    ? { text: ru.telegraph.offline, tone: "danger", glyph: "fault" as const }
    : bulkBusy
      ? { text: ru.telegraph.running, tone: "progress", glyph: "ring" as const }
      : faults > 0
        ? { text: ru.telegraph.alert(faults), tone: "danger", glyph: "fault" as const }
        : { text: ru.telegraph.nominal, tone: "success", glyph: "square" as const };

  const infra = managed.filter((c) => (snapshot?.composeFiles ?? []).some((f) => f.infra && f.path === c.composeFile));
  const rootName = snapshot?.root.split(/[\\/]/).filter(Boolean).pop() ?? "—";
  const drawerContainer = drawer?.kind === "container" ? ((snapshot?.containers ?? []).find((c) => c.id === drawer.id) ?? null) : null;
  const navItems: { route: Route; label: string; jp: string; icon: React.ReactNode; also?: Route[] }[] = [
    { route: "containers", label: ru.nav.containers, jp: ru.nav.containersJp, icon: <Boxes {...iconProps} aria-hidden /> },
    { route: "monitoring", label: ru.mon.nav, jp: ru.mon.navJp, icon: <Activity {...iconProps} aria-hidden /> },
    { route: "runs", label: rf.nav.runs, jp: rf.nav.runsJp, icon: <History {...iconProps} aria-hidden /> },
    { route: "backups", label: rf.nav.backups, jp: rf.nav.backupsJp, icon: <DatabaseBackup {...iconProps} aria-hidden /> },
    { route: "ports", label: rf.nav.network, jp: rf.nav.networkJp, icon: <Network {...iconProps} aria-hidden />, also: ["domains"] },
    { route: "infra", label: rf.nav.infra, jp: rf.nav.infraJp, icon: <FileCog {...iconProps} aria-hidden /> },
    { route: "cleanup", label: ru.cleanup.nav, jp: ru.cleanup.navJp, icon: <Eraser {...iconProps} aria-hidden /> },
    { route: "settings", label: ru.navMore.settings, jp: ru.navMore.settingsJp, icon: <Settings2 {...iconProps} aria-hidden /> },
  ];
  const isActive = (n: (typeof navItems)[number]) => route === n.route || (n.also ?? []).includes(route);
  const currentNav = navItems.find(isActive) ?? navItems[0]!;
  const startGuarded = (action: BulkAction) => (action === "stop" ? bulk.mutate(action) : guard({}, () => bulk.mutate(action)));
  const openContainer = (c: Container, tab: ContainerTab) => {
    go("containers");
    setDrawer({ kind: "container", id: c.id, tab });
  };

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.code === "KeyK") {
        e.preventDefault();
        setPaletteOpen((o) => !o);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const paletteItems = useMemo<PaletteItem[]>(() => {
    const items: PaletteItem[] = navItems.map((n) => ({ id: `nav-${n.route}`, group: "nav", label: rf.palette.go(n.label), icon: n.icon, run: () => go(n.route) }));
    items.push(
      { id: "act-start", group: "actions", label: rf.palette.startAll, hint: "sudoku start", icon: <Play {...iconProps} aria-hidden />, run: () => startGuarded("start") },
      { id: "act-restart", group: "actions", label: rf.palette.restartAll, hint: "sudoku restart", icon: <RotateCw {...iconProps} aria-hidden />, run: () => setPaletteConfirm("restart") },
      { id: "act-stop", group: "actions", label: rf.palette.stopAll, hint: "sudoku stop", icon: <Square {...iconProps} aria-hidden />, run: () => setPaletteConfirm("stop") },
      { id: "act-backups", group: "actions", label: rf.palette.backupAll, icon: <DatabaseBackup {...iconProps} aria-hidden />, run: () => go("backups") },
      { id: "act-doctor", group: "actions", label: rf.palette.doctor, hint: "sudoku doctor", icon: <Settings2 {...iconProps} aria-hidden />, run: () => go("settings") },
    );
    items.push({ id: "act-selfupdate", group: "actions", label: rf.prefs.check, hint: "sudoku update", icon: <RotateCw {...iconProps} aria-hidden />, run: () => go("settings") });
    if (settings.data?.canRestart) {
      items.push({ id: "act-restart-admin", group: "actions", label: rf.prefs.restart, icon: <RotateCw {...iconProps} aria-hidden />, run: restartAdmin.restart });
    }
    for (const c of managed) {
      const kw = `${c.service} ${c.project} ${c.name}`;
      const up = c.state === "running";
      items.push(
        { id: `logs-${c.id}`, group: "projects", label: rf.palette.logs(c.service), hint: c.name, keywords: kw, run: () => openContainer(c, "logs") },
        { id: `restart-${c.id}`, group: "projects", label: rf.palette.restart(c.service), hint: c.name, keywords: kw, run: () => containerAction.mutate({ c, action: "restart" }) },
        up
          ? { id: `stop-${c.id}`, group: "projects", label: rf.palette.stop(c.service), hint: c.name, keywords: kw, run: () => containerAction.mutate({ c, action: "stop" }) }
          : { id: `start-${c.id}`, group: "projects", label: rf.palette.start(c.service), hint: c.name, keywords: kw, run: () => containerAction.mutate({ c, action: "start" }) },
        { id: `term-${c.id}`, group: "projects", label: rf.palette.terminal(c.service), hint: c.name, keywords: kw, run: () => openContainer(c, "terminal") },
        { id: `cfg-${c.id}`, group: "projects", label: rf.palette.config(c.service), hint: c.name, keywords: kw, run: () => openContainer(c, "config") },
      );
    }
    return items;
  }, [managed, navItems, settings.data?.canRestart, restartAdmin.restart]);


  return (
    <div className="shell">
      <a className="skip" href="#main">
        {ru.page.title}
      </a>
      <aside className="sidebar">
        <div className="brand">
          <ProductMark />
          <div>
            <div className="brand-name">{ru.brand}</div>
            <div className="brand-sub mono">{ru.brandSub}</div>
          </div>
        </div>
        <nav className="nav" aria-label={ru.nav.containers}>
          {navItems.map((n) => (
            <a
              key={n.route}
              className={`nav-item ${isActive(n) ? "is-active" : ""}`}
              href={href(n.route)}
              aria-current={isActive(n) ? "page" : undefined}
              onClick={(e) => {
                if (e.metaKey || e.ctrlKey || e.shiftKey) return;
                e.preventDefault();
                go(n.route);
              }}
            >
              {n.icon}
              <span>{n.label}</span>
              <span className="jp" aria-hidden>
                {n.jp}
              </span>
            </a>
          ))}
        </nav>
        <div className="core" title={ru.coreHint}>
          <div className="core-label mono">{ru.core}</div>
          <ul className="core-list">
            {infra.map((c) => (
              <li key={c.id} className={`tone-${isFault(c.status) ? "danger" : c.status === "running" ? "success" : "neutral"}`}>
                <StatusGlyph status={c.status} size={10} label={ru.status[c.status]} />
                <span className="mono micro">{c.service}</span>
              </li>
            ))}
          </ul>
        </div>
        <div className="env">
          <span className="mono micro" title={snapshot?.root}>
            {rootName}
          </span>
          <span className={`env-link tone-${offline ? "danger" : "success"}`}>
            <StatusGlyph kind={offline ? "fault" : "square"} size={8} />
            <span className="mono micro">{offline ? ru.connection.offline : ru.connection.online}</span>
          </span>
        </div>
      </aside>

      <div className="main-col">
        <header className="topbar">
          <div className="crumbs">
            <span className="mobile-brand">
              <ProductMark size={20} />
            </span>
            <span>{ru.brand}</span>
            <span aria-hidden>›</span>
            <span className="crumb-current">{currentNav.label}</span>
          </div>
          <div className="topbar-right">
            <Button variant="ghost" size="sm" icon={<Command {...iconProps} />} title={`${rf.palette.title} (Ctrl+K)`} onClick={() => setPaletteOpen(true)}>
              {rf.palette.trigger}
              <kbd className="kbd">⌘K</kbd>
            </Button>
            {activeRun && (
              <Button variant="ghost" size="sm" icon={<ScrollText {...iconProps} />} onClick={() => setDrawer({ kind: "run" })}>
                {ru.run.open}
              </Button>
            )}
            <span className={`telegraph mono tone-${telegraph.tone}`} role="status" title={telegraph.text}>
              <span className={telegraph.glyph === "ring" ? "spin" : ""}>
                <StatusGlyph kind={telegraph.glyph} size={10} />
              </span>
              <span className="telegraph-text">{telegraph.text}</span>
            </span>
            <Clock />
            <Button
              variant="ghost"
              size="sm"
              iconOnly
              icon={theme === "dark" ? <Sun {...iconProps} /> : <Moon {...iconProps} />}
              onClick={toggleTheme}
              title={theme === "dark" ? ru.theme.toLight : ru.theme.toDark}
            >
              {theme === "dark" ? ru.theme.toLight : ru.theme.toDark}
            </Button>
          </div>
        </header>

        {offline && (
          <div className="offline" role="alert">
            <div className="hazard-band hazard-warning" aria-hidden />
            <span>{snapshot ? ru.offlineBanner(clock(state.dataUpdatedAt)) : ru.offlineBannerNoData}</span>
            <Button size="sm" variant="outline" onClick={() => void state.refetch()}>
              {ru.error.retry}
            </Button>
          </div>
        )}

        {route === "monitoring" ? (
          <MonitoringPage snapshot={snapshot} onOpen={(c) => setDrawer({ kind: "container", id: c.id, tab: "resources" })} onGoSettings={() => go("settings")} onGoCleanup={() => go("cleanup")} />
        ) : route === "cleanup" ? (
          <CleanupPage bulkBusy={bulkBusy} onGoBackups={() => go("backups")} />
        ) : route === "ports" ? (
          <PortsPage bulkBusy={bulkBusy} onRunStarted={onRunStarted} go={go} href={href} />
        ) : route === "domains" ? (
          <DomainsPage go={go} href={href} />
        ) : route === "infra" ? (
          <InfraPage bulkBusy={bulkBusy} onRunStarted={onRunStarted} />
        ) : route === "runs" ? (
          <RunsPage
            onOpen={(r) => {
              setRunId(r.id);
              setDrawer({ kind: "run" });
            }}
          />
        ) : route === "backups" ? (
          <BackupsPage bulkBusy={bulkBusy} onRunStarted={onRunStarted} />
        ) : route === "settings" ? (
          <SettingsPage />
        ) : snapshot ? (
          <ContainersPage
            snapshot={snapshot}
            updatedAt={state.dataUpdatedAt}
            pending={pending}
            bulkBusy={bulkBusy || offline}
            bulkPending={bulk.isPending ? (bulk.variables ?? null) : bulkBusy ? asBulk(activeRun?.action) : null}
            onBulk={startGuarded}
            guard={guard}
            onRunStarted={onRunStarted}
            onAction={(c, action) => containerAction.mutate({ c, action })}
            onOpen={(c, tab) => setDrawer({ kind: "container", id: c.id, tab })}
            onToggle={(file, service, disabled) => toggle.mutate({ file, service, disabled })}
            toggling={toggle.isPending && toggle.variables ? `${toggle.variables.file}::${toggle.variables.service}` : null}
            selectionPending={selection.isPending ? (selection.variables?.action ?? null) : null}
            onSelection={(action, targets, done) =>
              action === "start" || action === "restart"
                ? guard({ targets }, () => selection.mutate({ action, targets, done }))
                : selection.mutate({ action, targets, done })
            }
          />
        ) : state.isError ? (
          <main className="page" id="main">
            <div className="empty">
              <StatusGlyph kind="fault" size={32} />
              <div className="empty-code">{ru.error.title}</div>
              <p className="mono">{errorText(state.error)}</p>
              <Button variant="outline" onClick={() => void state.refetch()}>
                {ru.error.retry}
              </Button>
            </div>
          </main>
        ) : (
          <main className="page" id="main" aria-busy>
            <div className="skeleton-head" />
            <div className="skeleton-rows">
              {Array.from({ length: 8 }, (_, i) => (
                <div key={i} className="skeleton-row" />
              ))}
            </div>
          </main>
        )}
      </div>

      <RunDrawer run={activeRun} open={drawer?.kind === "run"} onOpenChange={setDrawerOpen} workers={snapshot?.maxWorkers ?? 1} />
      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} items={paletteItems} />
      {preflightDialog}
      <ConfirmDialog
        open={paletteConfirm !== null}
        onOpenChange={(o) => !o && setPaletteConfirm(null)}
        danger={paletteConfirm === "stop"}
        title={paletteConfirm === "stop" ? ru.confirm.stopAllTitle : ru.confirm.restartAllTitle}
        body={<p>{paletteConfirm === "stop" ? ru.confirm.stopAllBody(snapshot?.composeFiles?.length ?? 0) : ru.confirm.restartAllBody(snapshot?.composeFiles?.length ?? 0)}</p>}
        confirmLabel={paletteConfirm === "stop" ? ru.confirm.stopAllConfirm : ru.confirm.restartAllConfirm}
        confirmIcon={paletteConfirm === "stop" ? <Square {...iconProps} /> : <RotateCw {...iconProps} />}
        cli={paletteConfirm ? `sudoku ${paletteConfirm}` : undefined}
        onConfirm={() => paletteConfirm && startGuarded(paletteConfirm)}
      />
      <ContainerDrawer
        container={drawerContainer}
        tab={drawer?.kind === "container" ? drawer.tab : "logs"}
        onTab={(tab) => setDrawer((d) => (d?.kind === "container" ? { ...d, tab } : d))}
        open={drawer?.kind === "container"}
        onOpenChange={(o) => !o && setDrawer(null)}
        bulkBusy={bulkBusy}
        rebuilding={rebuild.isPending}
        onRebuild={(c, noCache) => rebuild.mutate({ c, noCache })}
        themeKey={theme}
      />
    </div>
  );
}
