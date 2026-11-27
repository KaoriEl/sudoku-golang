import { useMemo, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Ban, BookmarkPlus, CloudDownload, Layers, Play, Power, RotateCw, Search, Square, X } from "lucide-react";
import { ApiError, api } from "../../api/client";
import type { BulkAction, Container, ContainerAction, Run, SelectionAction, SelectionTarget, Snapshot } from "../../api/types";
import { useToast } from "../../components/ui/Toasts";
import { rf } from "../../i18n/ru.features";
import type { PreflightGuard } from "../preflight/Preflight";
import { SetsDialog } from "../sets/SetsDialog";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { Button } from "../../components/ui/Button";
import { Checkbox } from "../../components/ui/Checkbox";
import { ConfirmDialog } from "../../components/ui/ConfirmDialog";
import { Frame, SectionBar } from "../../components/ui/Frame";
import { ru } from "../../i18n/ru";
import { ago, isFault, isUp } from "../../lib/format";
import { useNow, usePref, useSearchParam } from "../../lib/hooks";
import type { ContainerTab } from "./ContainerDrawer";
import { ContainersTable, buildGroups, selectionKey, type MissingService } from "./ContainersTable";

const iconProps = { size: 16, strokeWidth: 2, strokeLinecap: "square" as const };

type Tab = "all" | "running" | "stopped" | "faults" | "disabled";

const tabFilter: Record<Tab, (c: Container) => boolean> = {
  all: () => true,
  running: (c) => isUp(c.status),
  stopped: (c) => !isUp(c.status),
  faults: (c) => isFault(c.status),
  disabled: (c) => c.disabled,
};

const missingTabs: Record<Tab, (m: MissingService) => boolean> = {
  all: () => true,
  running: () => false,
  stopped: () => true,
  faults: () => false,
  disabled: (m) => m.disabled,
};

function matches(c: Container, q: string): boolean {
  if (!q) return true;
  const hay = [c.name, c.service, c.project, c.image, c.hostname, ...(c.hosts ?? []), ...(c.networks ?? []).flatMap((n) => [n.ip, n.name, ...(n.dnsNames ?? [])])]
    .join(" ")
    .toLowerCase();
  return hay.includes(q.toLowerCase());
}

interface Props {
  snapshot: Snapshot;
  updatedAt: number;
  pending: Record<string, ContainerAction>;
  bulkBusy: boolean;
  bulkPending: BulkAction | null;
  onBulk: (action: BulkAction) => void;
  onAction: (c: Container, action: ContainerAction) => void;
  onOpen: (c: Container, tab: ContainerTab) => void;
  onToggle: (composeFile: string, service: string, disabled: boolean) => void;
  toggling: string | null;
  selectionPending: SelectionAction | null;
  onSelection: (action: SelectionAction, targets: SelectionTarget[], done: () => void) => void;
  guard: PreflightGuard;
  onRunStarted: (r: Run) => void;
}

function toTarget(key: string): SelectionTarget {
  const i = key.lastIndexOf("::");
  return { composeFile: key.slice(0, i), service: key.slice(i + 2) };
}

export function ContainersPage({
  snapshot,
  updatedAt,
  pending,
  bulkBusy,
  bulkPending,
  onBulk,
  onAction,
  onOpen,
  onToggle,
  toggling,
  selectionPending,
  onSelection,
  guard,
  onRunStarted,
}: Props) {
  const qc = useQueryClient();
  const toast = useToast();
  const [setsOpen, setSetsOpen] = useState(false);
  const [setDraft, setSetDraft] = useState<string[]>([]);
  const checkUpdates = useMutation({
    mutationFn: api.checkUpdates,
    onSuccess: (v) => {
      void qc.invalidateQueries({ queryKey: ["state"] });
      toast({ tone: v.available > 0 ? "progress" : "success", message: rf.updates.available(v.available), detail: v.available > 0 ? rf.updates.hint : undefined });
    },
    onError: (e) => toast({ tone: "danger", message: rf.updates.failed, detail: e instanceof ApiError ? e.message : String(e) }),
  });
  const now = useNow(1000);
  const [rawSelected, setSelected] = useState<Set<string>>(new Set());
  const [tab, setTab] = useSearchParam("tab", "all");
  const [query, setQuery] = useSearchParam("q", "");
  const [foreign, setForeign] = useSearchParam("foreign", "0");
  const [confirm, setConfirm] = useState<BulkAction | null>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const [skipStop, setSkipStop] = usePref("confirm.stopAll");
  const [skipRestart, setSkipRestart] = usePref("confirm.restartAll");
  const ask = (action: BulkAction) => {
    const skip = action === "stop" ? skipStop : action === "restart" ? skipRestart : true;
    if (skip) onBulk(action);
    else setConfirm(action);
  };

  const files = snapshot.composeFiles ?? [];
  const all = snapshot.containers ?? [];
  const managed = all.filter((c) => c.managed);
  const foreignCount = all.length - managed.length;
  const showForeign = foreign === "1";
  const scope = showForeign ? all : managed;
  const activeTab = (tab in tabFilter ? tab : "all") as Tab;

  const counts = useMemo(
    () => ({
      all: scope.length,
      running: scope.filter(tabFilter.running).length,
      stopped: scope.filter(tabFilter.stopped).length,
      faults: scope.filter(tabFilter.faults).length,
      disabled: files.reduce((n, f) => n + (f.disabled?.length ?? 0), 0),
    }),
    [scope, files],
  );

  const groups = useMemo(() => buildGroups(files, all, showForeign), [files, all, showForeign]);
  const validKeys = useMemo(() => {
    const keys = new Set<string>();
    for (const g of groups) {
      if (!g.file) continue;
      for (const c of g.containers) if (c.managed) keys.add(selectionKey(c.composeFile, c.service));
      for (const m of g.missing) keys.add(selectionKey(g.file.path, m.service));
    }
    return keys;
  }, [groups]);
  const selected = useMemo(() => new Set([...rawSelected].filter((k) => validKeys.has(k))), [rawSelected, validKeys]);
  const onSelect = (keys: string[], on: boolean) =>
    setSelected((prev) => {
      const next = new Set([...prev].filter((k) => validKeys.has(k)));
      for (const k of keys) {
        if (on) next.add(k);
        else next.delete(k);
      }
      return next;
    });
  const selectedTargets = [...selected].map(toTarget);
  const disabledOf = (t: SelectionTarget) => files.find((f) => f.path === t.composeFile)?.disabled?.includes(t.service) ?? false;
  const selDisabled = selectedTargets.filter(disabledOf).length;
  const runSelection = (action: SelectionAction) => onSelection(action, selectedTargets, () => setSelected(new Set()));
  const toSetTarget = (t: SelectionTarget) => {
    const f = files.find((x) => x.path === t.composeFile);
    return `${f?.infra ? "infra" : (f?.project ?? "")}/${t.service}`;
  };
  const selBusy = bulkBusy || selectionPending !== null;
  const filter = (c: Container) => tabFilter[activeTab](c) && matches(c, query.trim());
  const filterMissing = (m: MissingService) => missingTabs[activeTab](m) && (!query.trim() || m.service.toLowerCase().includes(query.trim().toLowerCase()));
  const filtering = activeTab !== "all" || query.trim() !== "";
  const anyVisible = groups.some((g) => g.containers.some(filter) || g.missing.some(filterMissing)) || (!filtering && groups.length > 0);
  const running = managed.filter((c) => isUp(c.status)).length;
  const busyReason = bulkBusy ? ru.bulk.busy : undefined;
  const fileList = files.map((f) => f.rel);

  return (
    <main className="page" id="main">
      <header className="page-head">
        <SectionBar title={ru.page.title} jp={ru.page.titleJp} level={1} count={`${managed.length} UNITS`}>
          <Button
            variant="danger-outline"
            icon={<Square {...iconProps} />}
            loading={bulkPending === "stop"}
            loadingLabel={ru.bulk.gerund.stop}
            disabled={bulkBusy || files.length === 0}
            disabledReason={busyReason}
            onClick={() => ask("stop")}
          >
            {ru.bulk.stopAll}
          </Button>
          <Button
            variant="outline"
            icon={<RotateCw {...iconProps} />}
            loading={bulkPending === "restart"}
            loadingLabel={ru.bulk.gerund.restart}
            disabled={bulkBusy || files.length === 0}
            disabledReason={busyReason}
            onClick={() => ask("restart")}
          >
            {ru.bulk.restartAll}
          </Button>
          <Button
            variant="primary"
            icon={<Play {...iconProps} />}
            loading={bulkPending === "start"}
            loadingLabel={ru.bulk.gerund.start}
            disabled={bulkBusy || files.length === 0}
            disabledReason={busyReason}
            onClick={() => onBulk("start")}
          >
            {ru.bulk.startAll}
          </Button>
        </SectionBar>
        <p className="page-summary">
          {ru.page.summary(running, managed.length)} · <span title={new Date(updatedAt).toISOString()}>{ru.page.updated(ago(updatedAt, now))}</span> ·{" "}
          <span className="mono micro" title={snapshot.root}>
            {snapshot.root}
          </span>
        </p>
      </header>

      <div className="toolbar" role="toolbar">
        <div className="tabs" role="tablist" aria-label={ru.page.title}>
          {(Object.keys(tabFilter) as Tab[]).map((t) => (
            <button
              key={t}
              type="button"
              role="tab"
              aria-selected={activeTab === t}
              className={`tab ${activeTab === t ? "is-active" : ""} ${t === "faults" && counts.faults > 0 ? "tab-alert" : ""}`}
              title={t === "disabled" ? ru.disabled.hint : undefined}
              onClick={() => setTab(t)}
            >
              <span className="tab-inner">
                {ru.tabs[t]} <span className="mono">{counts[t]}</span>
              </span>
            </button>
          ))}
        </div>
        <label className="search">
          <Search {...iconProps} size={14} aria-hidden />
          <span className="visually-hidden">{ru.search}</span>
          <input ref={searchRef} type="search" value={query} placeholder={ru.search} onChange={(e) => setQuery(e.target.value)} />
          {query && (
            <button type="button" className="search-clear" onClick={() => setQuery("")} aria-label={ru.empty.reset}>
              <X {...iconProps} size={14} />
            </button>
          )}
        </label>
        {foreignCount > 0 && (
          <Checkbox checked={showForeign} onChange={(v) => setForeign(v ? "1" : "0")}>
            {ru.showForeign(foreignCount)}
          </Checkbox>
        )}
        <div className="toolbar-actions">
          <Button
            size="sm"
            variant="ghost"
            icon={<Layers {...iconProps} size={14} />}
            onClick={() => {
              setSetDraft([]);
              setSetsOpen(true);
            }}
          >
            {rf.sets.button}
          </Button>
          <Button size="sm" variant="ghost" icon={<CloudDownload {...iconProps} size={14} />} loading={checkUpdates.isPending} loadingLabel={rf.updates.checking} onClick={() => checkUpdates.mutate()}>
            {rf.updates.check}
          </Button>
        </div>
      </div>

      <Frame flush label={ru.page.title}>
        {files.length === 0 && !showForeign ? (
          <div className="empty">
            <StatusGlyph kind="dashed" size={32} />
            <div className="empty-code">{ru.empty.noData}</div>
            <p>{ru.empty.noFiles}</p>
          </div>
        ) : anyVisible ? (
          <ContainersTable groups={groups} filter={filter} filtering={filtering} filterMissing={filterMissing}
            pending={pending}
            toggling={toggling}
            bulkBusy={bulkBusy}
            now={now}
            onAction={onAction}
            onOpen={onOpen}
            onToggle={onToggle}
            selected={selected}
            onSelect={onSelect}
          />
        ) : (
          <div className="empty">
            <StatusGlyph kind="dashed" size={32} />
            <div className="empty-code">{ru.empty.noData}</div>
            <p>{query ? ru.empty.noMatch(query) : ru.empty.noMatchTab}</p>
            <Button
              variant="outline"
              onClick={() => {
                setQuery("");
                setTab("all");
                searchRef.current?.focus();
              }}
            >
              {ru.empty.reset}
            </Button>
          </div>
        )}
      </Frame>

      {selected.size > 0 && (
        <div className="selection-bar" role="region" aria-label={ru.selection.title}>
          <div className="selection-count">
            <span className="mon-value mono">{selected.size}</span>
            <span className="meta">{ru.selection.count(selected.size)}</span>
          </div>
          <div className="selection-actions">
            <Button
              size="sm"
              variant="primary"
              icon={<Play {...iconProps} size={14} />}
              loading={selectionPending === "start"}
              disabled={selBusy}
              disabledReason={bulkBusy ? ru.bulk.busy : undefined}
              title={selDisabled > 0 ? ru.selection.startDisabledHint : undefined}
              onClick={() => runSelection("start")}
            >
              {ru.selection.start}
            </Button>
            <Button
              size="sm"
              variant="danger-outline"
              icon={<Square {...iconProps} size={14} />}
              loading={selectionPending === "stop"}
              disabled={selBusy}
              disabledReason={bulkBusy ? ru.bulk.busy : undefined}
              onClick={() => runSelection("stop")}
            >
              {ru.selection.stop}
            </Button>
            <Button
              size="sm"
              variant="outline"
              icon={<RotateCw {...iconProps} size={14} />}
              loading={selectionPending === "restart"}
              disabled={selBusy}
              disabledReason={bulkBusy ? ru.bulk.busy : undefined}
              onClick={() => runSelection("restart")}
            >
              {ru.selection.restart}
            </Button>
            <span className="selection-sep" aria-hidden />
            <Button
              size="sm"
              variant="outline"
              icon={<Ban {...iconProps} size={14} />}
              loading={selectionPending === "disable"}
              disabled={selectionPending !== null || selDisabled === selected.size}
              onClick={() => runSelection("disable")}
            >
              {ru.selection.disable}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              icon={<Power {...iconProps} size={14} />}
              loading={selectionPending === "enable"}
              disabled={selectionPending !== null || selDisabled === 0}
              onClick={() => runSelection("enable")}
            >
              {ru.selection.enable}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              icon={<BookmarkPlus {...iconProps} size={14} />}
              onClick={() => {
                setSetDraft(selectedTargets.map(toSetTarget));
                setSetsOpen(true);
              }}
            >
              {rf.sets.saveSelection}
            </Button>
            <Button size="sm" variant="ghost" iconOnly icon={<X {...iconProps} size={14} />} title={ru.selection.clear} onClick={() => setSelected(new Set())}>
              {ru.selection.clear}
            </Button>
          </div>
        </div>
      )}

      <SetsDialog open={setsOpen} onOpenChange={setSetsOpen} draftTargets={setDraft} bulkBusy={bulkBusy} guard={guard} onRunStarted={onRunStarted} />
      <ConfirmDialog
        open={confirm === "stop"}
        onOpenChange={(o) => !o && setConfirm(null)}
        danger
        title={ru.confirm.stopAllTitle}
        body={<FilesBody text={ru.confirm.stopAllBody(files.length)} files={fileList} />}
        confirmLabel={ru.confirm.stopAllConfirm}
        confirmIcon={<Square {...iconProps} />}
        cli="sudoku stop"
        offerDontAsk
        onConfirm={(dontAsk) => {
          if (dontAsk) setSkipStop(true);
          onBulk("stop");
        }}
      />
      <ConfirmDialog
        open={confirm === "restart"}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={ru.confirm.restartAllTitle}
        body={<FilesBody text={ru.confirm.restartAllBody(files.length)} files={fileList} />}
        confirmLabel={ru.confirm.restartAllConfirm}
        confirmIcon={<RotateCw {...iconProps} />}
        cli="sudoku restart"
        offerDontAsk
        onConfirm={(dontAsk) => {
          if (dontAsk) setSkipRestart(true);
          onBulk("restart");
        }}
      />
    </main>
  );
}

function FilesBody({ text, files }: { text: string; files: string[] }) {
  const shown = files.slice(0, 5);
  return (
    <>
      <p>{text}</p>
      <p className="meta">{ru.confirm.filesList}</p>
      <ul className="file-list mono">
        {shown.map((f) => (
          <li key={f}>{f}</li>
        ))}
        {files.length > shown.length && <li className="meta">{ru.confirm.andMore(files.length - shown.length)}</li>}
      </ul>
    </>
  );
}
