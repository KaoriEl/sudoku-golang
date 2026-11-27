import * as Dialog from "@radix-ui/react-dialog";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { rf } from "../../i18n/ru.features";

export type PaletteGroup = "nav" | "projects" | "actions";

export interface PaletteItem {
  id: string;
  group: PaletteGroup;
  label: string;
  hint?: string;
  icon?: ReactNode;
  keywords?: string;
  run: () => void;
}

const MAX_SHOWN = 60;
const groupOrder: PaletteGroup[] = ["nav", "actions", "projects"];

function fuzzy(word: string, q: string): number {
  let pos = 0;
  let gaps = 0;
  for (const ch of q) {
    const next = word.indexOf(ch, pos);
    if (next < 0) return -1;
    gaps += next - pos;
    pos = next + 1;
  }
  return gaps;
}

function score(label: string, extra: string, q: string): number {
  if (!q) return 1;
  const li = label.indexOf(q);
  if (li >= 0) return 2000 - li;
  const ei = extra.indexOf(q);
  if (ei >= 0) return 1000 - ei;
  let best = 0;
  for (const word of label.split(/[\s/_.-]+/)) {
    const gaps = fuzzy(word, q);
    if (gaps >= 0 && gaps <= q.length) best = Math.max(best, 500 - gaps);
  }
  return best;
}

export function CommandPalette({ open, onOpenChange, items }: { open: boolean; onOpenChange: (o: boolean) => void; items: PaletteItem[] }) {
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const listRef = useRef<HTMLUListElement>(null);

  useEffect(() => {
    if (open) {
      setQuery("");
      setActive(0);
    }
  }, [open]);

  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    const scored = items
      .map((it) => ({ it, s: score(it.label.toLowerCase(), `${it.hint ?? ""} ${it.keywords ?? ""}`.toLowerCase(), q) }))
      .filter((x) => x.s > 0 && (q !== "" || x.it.group !== "projects"));
    scored.sort((a, b) => (q ? b.s - a.s : groupOrder.indexOf(a.it.group) - groupOrder.indexOf(b.it.group)));
    return scored.slice(0, MAX_SHOWN).map((x) => x.it);
  }, [items, query]);

  useEffect(() => setActive(0), [query]);
  useEffect(() => {
    listRef.current?.querySelector(`[data-idx="${active}"]`)?.scrollIntoView({ block: "nearest" });
  }, [active]);

  const exec = (it: PaletteItem | undefined) => {
    if (!it) return;
    onOpenChange(false);
    it.run();
  };

  let lastGroup: PaletteGroup | null = null;
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="scrim" />
        <Dialog.Content className="dialog palette" aria-describedby={undefined}>
          <span className="bracket bracket-tr" aria-hidden />
          <span className="bracket bracket-bl" aria-hidden />
          <Dialog.Title className="visually-hidden">{rf.palette.title}</Dialog.Title>
          <div className="palette-input">
            <span className="palette-prompt mono" aria-hidden>
              »
            </span>
            <input
              className="mono"
              autoFocus
              value={query}
              placeholder={rf.palette.placeholder}
              role="combobox"
              aria-expanded
              aria-controls="palette-list"
              aria-activedescendant={shown[active] ? `palette-${shown[active].id}` : undefined}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "ArrowDown") {
                  e.preventDefault();
                  setActive((a) => Math.min(shown.length - 1, a + 1));
                } else if (e.key === "ArrowUp") {
                  e.preventDefault();
                  setActive((a) => Math.max(0, a - 1));
                } else if (e.key === "Enter") {
                  e.preventDefault();
                  exec(shown[active]);
                }
              }}
            />
          </div>
          <ul id="palette-list" ref={listRef} className="palette-list" role="listbox">
            {shown.length === 0 && <li className="palette-empty meta">{rf.palette.empty}</li>}
            {shown.map((it, i) => {
              const head = it.group !== lastGroup;
              lastGroup = it.group;
              return (
                <li key={it.id} role="presentation">
                  {head && (
                    <div className="palette-group mono micro" aria-hidden>
                      {rf.palette.groups[it.group]}
                    </div>
                  )}
                  <button
                    type="button"
                    id={`palette-${it.id}`}
                    role="option"
                    aria-selected={i === active}
                    data-idx={i}
                    className={`palette-item ${i === active ? "is-active" : ""}`}
                    onMouseMove={() => setActive(i)}
                    onClick={() => exec(it)}
                  >
                    {it.icon}
                    <span className="palette-label">{it.label}</span>
                    {it.hint && <span className="palette-hint mono micro">{it.hint}</span>}
                  </button>
                </li>
              );
            })}
          </ul>
          <div className="palette-foot meta micro">{rf.palette.hint}</div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
