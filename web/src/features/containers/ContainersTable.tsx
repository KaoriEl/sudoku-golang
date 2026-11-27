import type { ComposeFile, Container, ContainerAction } from "../../api/types";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { Checkbox } from "../../components/ui/Checkbox";
import { ru } from "../../i18n/ru";
import { isUp } from "../../lib/format";
import { ContainerRow } from "./ContainerRow";
import { DisabledBadge, ToggleButton } from "./DisabledControls";
import type { ContainerTab } from "./ContainerDrawer";

export interface MissingService {
  service: string;
  disabled: boolean;
}

export interface Group {
  key: string;
  file: ComposeFile | null;
  title: string;
  subtitle: string;
  infra: boolean;
  foreign: boolean;
  containers: Container[];
  missing: MissingService[];
  disabledCount: number;
  total: number;
}

export function buildGroups(files: ComposeFile[], containers: Container[], showForeign: boolean): Group[] {
  const byFile = new Map<string, Container[]>();
  for (const c of containers) {
    const key = c.managed ? c.composeFile : "__foreign__";
    const list = byFile.get(key) ?? [];
    list.push(c);
    byFile.set(key, list);
  }

  const groups: Group[] = files.map((f) => {
    const list = byFile.get(f.path) ?? [];
    const present = new Set(list.map((c) => c.service));
    const disabled = new Set(f.disabled ?? []);
    const missing = (f.services ?? []).filter((s) => !present.has(s)).map((s) => ({ service: s, disabled: disabled.has(s) }));
    return {
      key: f.path,
      file: f,
      title: f.infra ? ru.group.infra : f.project,
      subtitle: f.rel,
      infra: f.infra,
      foreign: false,
      containers: list,
      missing,
      disabledCount: disabled.size,
      total: list.length,
    };
  });

  groups.sort((a, b) => Number(b.infra) - Number(a.infra));

  const foreign = byFile.get("__foreign__") ?? [];
  if (showForeign && foreign.length > 0) {
    groups.push({
      key: "__foreign__",
      file: null,
      title: ru.group.foreign,
      subtitle: ru.group.foreignHint,
      infra: false,
      foreign: true,
      containers: foreign,
      missing: [],
      disabledCount: 0,
      total: foreign.length,
    });
  }
  return groups;
}

interface Props {
  groups: Group[];
  filter: (c: Container) => boolean;
  filterMissing: (m: MissingService) => boolean;
  filtering: boolean;
  pending: Record<string, ContainerAction>;
  toggling: string | null;
  bulkBusy: boolean;
  now: number;
  onAction: (c: Container, action: ContainerAction) => void;
  onOpen: (c: Container, tab: ContainerTab) => void;
  onToggle: (composeFile: string, service: string, disabled: boolean) => void;
  selected: Set<string>;
  onSelect: (keys: string[], selected: boolean) => void;
}

export function selectionKey(composeFile: string, service: string): string {
  return `${composeFile}::${service}`;
}

function Tri({ keys, selected, onSelect, label }: { keys: string[]; selected: Set<string>; onSelect: Props["onSelect"]; label: string }) {
  const n = keys.filter((k) => selected.has(k)).length;
  if (keys.length === 0) return null;
  return (
    <Checkbox className={n > 0 && n < keys.length ? "is-mixed" : ""} checked={n === keys.length} onChange={(v) => onSelect(keys, v)} title={label}>
      <span className="visually-hidden">{label}</span>
    </Checkbox>
  );
}

export function ContainersTable({
  groups,
  filter,
  filterMissing,
  filtering,
  pending,
  toggling,
  bulkBusy,
  now,
  onAction,
  onOpen,
  onToggle,
  selected,
  onSelect,
}: Props) {
  const visible = groups
    .map((g) => ({ ...g, rows: g.containers.filter(filter), missingRows: g.missing.filter(filterMissing) }))
    .filter((g) => g.rows.length > 0 || g.missingRows.length > 0 || (!filtering && !g.foreign));

  if (visible.length === 0) return null;

  const groupKeys = (g: (typeof visible)[number]) =>
    g.file
      ? [...new Set([...g.rows.filter((c) => c.managed).map((c) => selectionKey(c.composeFile, c.service)), ...g.missingRows.map((m) => selectionKey(g.file!.path, m.service))])]
      : [];
  const allKeys = [...new Set(visible.flatMap(groupKeys))];

  return (
    <div className="table-scroll">
      <table className="table">
        <thead>
          <tr>
            <th scope="col" className="th-check">
              <Tri keys={allKeys} selected={selected} onSelect={onSelect} label={ru.selection.allVisible} />
            </th>
            <th scope="col">{ru.columns.status}</th>
            <th scope="col">{ru.columns.container}</th>
            <th scope="col">{ru.columns.address}</th>
            <th scope="col">{ru.columns.ports}</th>
            <th scope="col">{ru.columns.hosts}</th>
            <th scope="col" className="th-actions">
              {ru.columns.actions}
            </th>
          </tr>
        </thead>
        {visible.map((g) => {
          const running = g.containers.filter((c) => isUp(c.status)).length;
          return (
            <tbody key={g.key} className={`group ${g.foreign ? "group-foreign" : ""}`}>
              <tr className="group-row">
                <th colSpan={7} scope="rowgroup">
                  <div className="group-head">
                    <span className="group-check">
                      <Tri keys={groupKeys(g)} selected={selected} onSelect={onSelect} label={ru.selection.group(g.title)} />
                    </span>
                    <span className="group-chevron" aria-hidden>
                      »
                    </span>
                    <span className="group-title">{g.title}</span>
                    <span className="group-sub mono micro" title={g.subtitle}>
                      {g.subtitle}
                    </span>
                    {g.disabledCount > 0 && (
                      <span className="chip mono micro" title={ru.disabled.hint}>
                        {ru.disabled.groupCount(g.disabledCount)}
                      </span>
                    )}
                    {g.file?.error && (
                      <span className="tone-danger micro" title={g.file.error}>
                        <StatusGlyph kind="fault" size={10} />
                      </span>
                    )}
                    <span className="group-gauge" aria-label={ru.group.running(running, g.total)}>
                      {g.containers.slice(0, 12).map((c) => (
                        <span key={c.id} className={`seg seg-${c.status}`} aria-hidden />
                      ))}
                      <span className="mono micro">{ru.group.running(running, g.total)}</span>
                    </span>
                  </div>
                </th>
              </tr>
              {g.rows.length === 0 && g.missingRows.length === 0 && (
                <tr className="row-empty">
                  <td colSpan={7}>
                    <StatusGlyph kind="dashed" size={12} />
                    <span>{ru.group.empty}</span>
                  </td>
                </tr>
              )}
              {g.rows.map((c) => (
                <ContainerRow
                  key={c.id}
                  container={c}
                  pending={pending[c.id] ?? null}
                  toggling={toggling === `${c.composeFile}::${c.service}`}
                  bulkBusy={bulkBusy}
                  now={now}
                  onAction={onAction}
                  onOpen={onOpen}
                  onToggle={(disabled) => onToggle(c.composeFile, c.service, disabled)}
                  selected={c.managed && selected.has(selectionKey(c.composeFile, c.service))}
                  onSelect={c.managed ? (v) => onSelect([selectionKey(c.composeFile, c.service)], v) : null}
                />
              ))}
              {g.file &&
                g.missingRows.map((m) => (
                  <tr
                    key={m.service}
                    className={`row row-missing ${m.disabled ? "row-disabled" : ""} ${selected.has(selectionKey(g.file!.path, m.service)) ? "is-selected" : ""}`}
                  >
                    <td className="cell-check">
                      <Checkbox
                        checked={selected.has(selectionKey(g.file!.path, m.service))}
                        onChange={(v) => onSelect([selectionKey(g.file!.path, m.service)], v)}
                      >
                        <span className="visually-hidden">{ru.selection.select(m.service)}</span>
                      </Checkbox>
                    </td>
                    <td className="cell-status" data-label={ru.columns.status}>
                      <span className="badge badge-neutral" title={ru.disabled.notCreatedHint}>
                        <StatusGlyph kind="dashed" size={10} />
                        {ru.disabled.notCreated}
                      </span>
                      {m.disabled && <DisabledBadge />}
                    </td>
                    <td className="cell-name" data-label={ru.columns.container}>
                      <span className="mono name-missing">{m.service}</span>
                      <div className="meta">{ru.container.service}</div>
                    </td>
                    <td className="meta" colSpan={3}>
                      —
                    </td>
                    <td className="cell-actions">
                      <div className="row-actions">
                        <ToggleButton
                          disabled={m.disabled}
                          loading={toggling === `${g.file!.path}::${m.service}`}
                          onClick={() => onToggle(g.file!.path, m.service, !m.disabled)}
                        />
                      </div>
                    </td>
                  </tr>
                ))}
            </tbody>
          );
        })}
      </table>
    </div>
  );
}
