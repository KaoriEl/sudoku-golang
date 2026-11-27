import { ArrowUpRight, Hammer, Play, RotateCw, ScrollText, Square, SquareTerminal } from "lucide-react";
import type { Container, ContainerAction } from "../../api/types";
import { Button } from "../../components/ui/Button";
import { Checkbox } from "../../components/ui/Checkbox";
import { CopyText } from "../../components/ui/CopyButton";
import { ru } from "../../i18n/ru";
import { rf } from "../../i18n/ru.features";
import { ago, dnsAliases, hostLabel, isUp, portUrl } from "../../lib/format";
import type { ContainerTab } from "./ContainerDrawer";
import { DisabledBadge, ToggleButton } from "./DisabledControls";
import { StatusBadge } from "./StatusBadge";

const iconProps = { size: 14, strokeWidth: 2, strokeLinecap: "square" as const };

interface Props {
  container: Container;
  pending: ContainerAction | null;
  bulkBusy: boolean;
  now: number;
  onAction: (c: Container, action: ContainerAction) => void;
  onOpen: (c: Container, tab: ContainerTab) => void;
  onToggle: (disabled: boolean) => void;
  toggling: boolean;
  selected: boolean;
  onSelect: ((selected: boolean) => void) | null;
}

export function ContainerRow({ container: c, pending, bulkBusy, now, onAction, onOpen, onToggle, toggling, selected, onSelect }: Props) {
  const up = isUp(c.status);
  const firstIp = c.networks?.find((n) => n.ip)?.ip ?? "";
  const disabledReason = !c.managed ? ru.actions.foreign : bulkBusy ? ru.actions.busy : undefined;
  const disabled = Boolean(disabledReason) || pending !== null;
  const since = up ? ru.container.uptime(ago(c.startedAt, now)) : c.finishedAt ? ru.container.downtime(ago(c.finishedAt, now)) : "";

  return (
    <tr className={`row row-${c.status} ${c.managed ? "" : "row-foreign"} ${c.disabled ? "row-disabled" : ""} ${selected ? "is-selected" : ""}`}>
      <td className="cell-check">
        {onSelect && (
          <Checkbox checked={selected} onChange={onSelect}>
            <span className="visually-hidden">{ru.selection.select(c.name)}</span>
          </Checkbox>
        )}
      </td>
      <td className="cell-status" data-label={ru.columns.status}>
        <StatusBadge container={c} pending={pending !== null} />
        {c.disabled && <DisabledBadge />}
        {c.disabled && up && <div className="meta tone-warning">{ru.disabled.stillRunning}</div>}
        {since && <div className="meta">{since}</div>}
      </td>

      <td className="cell-name" data-label={ru.columns.container}>
        <CopyText value={c.name} className="name">
          {c.name}
        </CopyText>
        <div className="meta">
          <span>
            {ru.container.service} <b>{c.service}</b>
          </span>
          <span className="mono micro" title={c.id}>
            {ru.container.id} {c.id.slice(0, 12)}
          </span>
        </div>
        <div className="meta mono micro image" title={c.image}>
          {c.image}
          {c.update?.available && (
            <span className="badge badge-progress update-badge" title={`${rf.updates.title(c.image)} · ${rf.updates.hint}`}>
              {rf.updates.badge}
            </span>
          )}
        </div>
      </td>

      <td className="cell-address" data-label={ru.columns.address}>
        {(c.networks ?? []).length === 0 && <span className="meta">{ru.container.noNetwork}</span>}
        {(c.networks ?? []).map((n) => {
          const aliases = dnsAliases(n.dnsNames, c.name);
          return (
            <div key={n.name} className="net">
              {n.ip ? (
                <CopyText value={n.ip} className="ip mono">
                  {n.ip}
                </CopyText>
              ) : (
                <span className="meta">—</span>
              )}
              <span className="net-name mono micro" title={ru.container.ipHint(n.name)}>
                {n.name}
              </span>
              {aliases.length > 0 && (
                <div className="meta micro">
                  {ru.container.dns}: <span className="mono">{aliases.join(", ")}</span>
                </div>
              )}
            </div>
          );
        })}
      </td>

      <td className="cell-ports" data-label={ru.columns.ports}>
        {(c.ports ?? []).length === 0 && <span className="meta">{ru.container.noPorts}</span>}
        <ul className="ports">
          {(c.ports ?? []).map((p) => {
            const url = portUrl(p);
            const key = `${p.containerPort}/${p.protocol}-${p.hostPort}`;
            if (p.hostPort > 0) {
              const label = `${hostLabel(p.hostIp)}:${p.hostPort}`;
              return (
                <li key={key} className="port port-published" title={ru.container.publishedHint(label)}>
                  {url ? (
                    <a href={url} target="_blank" rel="noreferrer" className="mono">
                      {label}
                    </a>
                  ) : (
                    <span className="mono">{label}</span>
                  )}
                  <span className="port-arrow" aria-hidden>
                    →
                  </span>
                  <span className="mono micro">
                    {p.containerPort}/{p.protocol}
                  </span>
                </li>
              );
            }
            return (
              <li key={key} className="port port-internal">
                {firstIp ? (
                  <CopyText value={`${firstIp}:${p.containerPort}`} className="mono micro">
                    <span title={ru.container.internalPortHint(firstIp, p.containerPort)}>
                      {p.containerPort}/{p.protocol}
                    </span>
                  </CopyText>
                ) : (
                  <span className="mono micro">
                    {p.containerPort}/{p.protocol}
                  </span>
                )}
              </li>
            );
          })}
        </ul>
      </td>

      <td className="cell-hosts" data-label={ru.columns.hosts}>
        {(c.hosts ?? []).length === 0 && <span className="meta">—</span>}
        {(c.hosts ?? []).map((h) => (
          <a key={h} href={`http://${h}`} target="_blank" rel="noreferrer" className="host mono">
            {h}
            <ArrowUpRight {...iconProps} size={12} aria-hidden />
          </a>
        ))}
      </td>

      <td className="cell-actions" data-label={ru.columns.actions}>
        <div className="row-actions">
          {up ? (
            <Button
              size="sm"
              variant="outline"
              icon={<Square {...iconProps} />}
              loading={pending === "stop"}
              loadingLabel={ru.actions.gerund.stop}
              disabled={disabled}
              disabledReason={disabledReason}
              onClick={() => onAction(c, "stop")}
            >
              {ru.actions.stop}
            </Button>
          ) : (
            <Button
              size="sm"
              variant="outline"
              icon={<Play {...iconProps} />}
              loading={pending === "start"}
              loadingLabel={ru.actions.gerund.start}
              disabled={disabled}
              disabledReason={disabledReason}
              onClick={() => onAction(c, "start")}
            >
              {ru.actions.start}
            </Button>
          )}
          <Button
            size="sm"
            variant="ghost"
            iconOnly
            title={ru.actions.restart}
            icon={<RotateCw {...iconProps} />}
            loading={pending === "restart"}
            loadingLabel={ru.actions.gerund.restart}
            disabled={disabled}
            disabledReason={disabledReason}
            onClick={() => onAction(c, "restart")}
          >
            {ru.actions.restart}
          </Button>
          <span className="row-actions-sep" aria-hidden />
          <Button size="sm" variant="ghost" iconOnly title={ru.row.logs} icon={<ScrollText {...iconProps} />} onClick={() => onOpen(c, "logs")}>
            {ru.row.logs}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            iconOnly
            title={ru.row.terminal}
            icon={<SquareTerminal {...iconProps} />}
            disabled={!c.managed || c.state !== "running"}
            disabledReason={!c.managed ? ru.term.foreign : ru.row.terminalOff}
            onClick={() => onOpen(c, "terminal")}
          >
            {ru.row.terminal}
          </Button>
          <Button size="sm" variant="ghost" iconOnly title={ru.row.build} icon={<Hammer {...iconProps} />} onClick={() => onOpen(c, "build")}>
            {ru.row.build}
          </Button>
          {c.managed && <ToggleButton disabled={c.disabled} loading={toggling} onClick={() => onToggle(!c.disabled)} />}
        </div>
      </td>
    </tr>
  );
}
