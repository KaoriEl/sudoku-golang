import * as Dialog from "@radix-ui/react-dialog";
import { Activity, FileCode2, Hammer, ScrollText, SquareTerminal, X } from "lucide-react";
import { lazy, Suspense } from "react";
import type { Container } from "../../api/types";
import { SectionBar } from "../../components/ui/Frame";
import { ru } from "../../i18n/ru";
import { ResourcesPanel } from "../monitoring/ResourcesPanel";
import { rf } from "../../i18n/ru.features";
import { BuildPanel } from "./BuildPanel";
import { ConfigPanel } from "./ConfigPanel";
import { LogViewer } from "./LogViewer";
import { StatusBadge } from "./StatusBadge";


const Terminal = lazy(() => import("./Terminal").then((m) => ({ default: m.Terminal })));

export type ContainerTab = "logs" | "resources" | "terminal" | "build" | "config";

const iconProps = { size: 14, strokeWidth: 2, strokeLinecap: "square" as const };

interface Props {
  container: Container | null;
  tab: ContainerTab;
  onTab: (t: ContainerTab) => void;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  bulkBusy: boolean;
  rebuilding: boolean;
  onRebuild: (c: Container, noCache: boolean) => void;
  themeKey: string;
}

export function ContainerDrawer({ container: c, tab, onTab, open, onOpenChange, bulkBusy, rebuilding, onRebuild, themeKey }: Props) {
  const running = c?.state === "running";
  const termReason = !c?.managed ? ru.term.foreign : !running ? ru.term.notRunning : "";
  const tabs: { id: ContainerTab; label: string; icon: React.ReactNode }[] = [
    { id: "logs", label: ru.drawer.tabs.logs, icon: <ScrollText {...iconProps} /> },
    { id: "resources", label: ru.drawer.tabs.resources, icon: <Activity {...iconProps} /> },
    { id: "terminal", label: ru.drawer.tabs.terminal, icon: <SquareTerminal {...iconProps} /> },
    { id: "build", label: ru.drawer.tabs.build, icon: <Hammer {...iconProps} /> },
    ...(c?.managed ? [{ id: "config" as const, label: rf.config.tab, icon: <FileCode2 {...iconProps} /> }] : []),
  ];

  return (
    <Dialog.Root open={open && c !== null} onOpenChange={onOpenChange} modal={false}>
      <Dialog.Portal>
        <Dialog.Content
          className="drawer drawer-wide"
          onInteractOutside={(e) => e.preventDefault()}
          onOpenAutoFocus={(e) => e.preventDefault()}
          aria-describedby={undefined}
        >
          <span className="bracket bracket-tl" aria-hidden />
          <span className="bracket bracket-bl" aria-hidden />
          {c && (
            <>
              <div className="drawer-head">
                <SectionBar title={c.name} level={2}>
                  <Dialog.Close className="btn btn-ghost btn-sm btn-icon" aria-label={ru.drawer.close} title={ru.drawer.close}>
                    <X size={16} strokeWidth={2} strokeLinecap="square" />
                  </Dialog.Close>
                </SectionBar>
                <Dialog.Title className="visually-hidden">{c.name}</Dialog.Title>
                <div className="drawer-meta">
                  <StatusBadge container={c} />
                  <span className="meta">
                    {ru.container.service} <b>{c.service}</b>
                  </span>
                  <span className="mono micro meta">{c.image}</span>
                </div>
                <div className="tabs drawer-tabs" role="tablist">
                  {tabs.map((t) => (
                    <button
                      key={t.id}
                      type="button"
                      role="tab"
                      aria-selected={tab === t.id}
                      className={`tab ${tab === t.id ? "is-active" : ""}`}
                      onClick={() => onTab(t.id)}
                    >
                      <span className="tab-inner">
                        {t.icon}
                        {t.label}
                      </span>
                    </button>
                  ))}
                </div>
              </div>
              <div className="drawer-body drawer-body-fill" role="tabpanel">
                {tab === "resources" && <ResourcesPanel container={c} />}
                {tab === "logs" && <LogViewer key={c.id} container={c} />}
                {tab === "config" && c.managed && <ConfigPanel key={c.id} container={c} />}
                {tab === "terminal" &&
                  (termReason ? (
                    <div className="empty">
                      <div className="empty-code">{ru.term.closed}</div>
                      <p>{termReason}</p>
                    </div>
                  ) : (
                    <Suspense fallback={<div className="skeleton-row" />}>
                      <Terminal key={`${c.id}-${c.startedAt}`} containerId={c.id} themeKey={themeKey} />
                    </Suspense>
                  ))}
                {tab === "build" && <BuildPanel container={c} bulkBusy={bulkBusy} rebuilding={rebuilding} onRebuild={(noCache) => onRebuild(c, noCache)} />}
              </div>
            </>
          )}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
