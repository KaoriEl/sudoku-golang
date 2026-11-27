import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { DownloadCloud, Play, Plus, RotateCcw, Save } from "lucide-react";
import { ApiError, api } from "../../api/client";
import type { InfraFile, InfraFileId, InfraFilesView, Run } from "../../api/types";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { Button } from "../../components/ui/Button";
import { ConfirmDialog } from "../../components/ui/ConfirmDialog";
import { CopyText } from "../../components/ui/CopyButton";
import { Frame, SectionBar } from "../../components/ui/Frame";
import { useToast } from "../../components/ui/Toasts";
import { ru } from "../../i18n/ru";
import { rf } from "../../i18n/ru.features";
import { detectPrefix, hasService, insertWithDeps, snippetGroups, snippets } from "./snippets";

const t = rf.infra;
const iconProps = { size: 14, strokeWidth: 2, strokeLinecap: "square" as const };
const ids: InfraFileId[] = ["compose", "traefik"];
const INDENT = "  ";

interface Draft {
  base: string;
  text: string;
}

const drafts = new Map<InfraFileId, Draft>();

function errorText(e: unknown): string {
  return e instanceof ApiError ? e.message : String(e);
}

function isStale(e: unknown): boolean {
  return e instanceof ApiError && e.status === 409 && e.code === "conflict";
}

function CodeEditor({
  value,
  onChange,
  onSave,
  disabled,
  label,
  selection,
}: {
  value: string;
  onChange: (v: string) => void;
  onSave: () => void;
  disabled: boolean;
  label: string;
  selection: { start: number; end: number; seq: number } | null;
}) {
  const area = useRef<HTMLTextAreaElement>(null);
  const gutter = useRef<HTMLDivElement>(null);
  const lines = useMemo(() => value.split("\n").length, [value]);

  useEffect(() => {
    const el = area.current;
    if (!el || !selection) return;
    el.focus();
    el.setSelectionRange(selection.start, selection.end);
    const before = value.slice(0, selection.start).split("\n").length - 1;
    const lineHeight = parseFloat(getComputedStyle(el).lineHeight) || 20;
    el.scrollTop = Math.max(0, before * lineHeight - el.clientHeight / 3);
  }, [selection]);

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if ((e.metaKey || e.ctrlKey) && e.code === "KeyS") {
      e.preventDefault();
      onSave();
      return;
    }
    if (e.key !== "Tab" || e.altKey || e.metaKey || e.ctrlKey) return;
    e.preventDefault();
    const el = e.currentTarget;
    const { selectionStart: s, selectionEnd: end } = el;
    const lineStart = value.lastIndexOf("\n", s - 1) + 1;
    if (s === end && !e.shiftKey) {
      onChange(value.slice(0, s) + INDENT + value.slice(end));
      requestAnimationFrame(() => el.setSelectionRange(s + INDENT.length, s + INDENT.length));
      return;
    }
    const block = value.slice(lineStart, end);
    const next = e.shiftKey ? block.replace(/^ {1,2}/gm, "") : block.replace(/^/gm, INDENT);
    onChange(value.slice(0, lineStart) + next + value.slice(end));
    requestAnimationFrame(() => el.setSelectionRange(lineStart, lineStart + next.length));
  };

  return (
    <div className="code-editor">
      <div className="code-gutter mono" ref={gutter} aria-hidden>
        {Array.from({ length: lines }, (_, i) => (
          <div key={i}>{i + 1}</div>
        ))}
      </div>
      <textarea
        ref={area}
        className="code-area mono"
        value={value}
        aria-label={label}
        spellCheck={false}
        autoCapitalize="off"
        autoComplete="off"
        autoCorrect="off"
        wrap="off"
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={onKeyDown}
        onScroll={(e) => {
          if (gutter.current) gutter.current.scrollTop = e.currentTarget.scrollTop;
        }}
      />
    </div>
  );
}

function FileTabs({ active, dirty, onPick }: { active: InfraFileId; dirty: (id: InfraFileId) => boolean; onPick: (id: InfraFileId) => void }) {
  return (
    <div className="tabs page-tabs" role="tablist" aria-label={t.title}>
      {ids.map((id) => (
        <button
          key={id}
          type="button"
          role="tab"
          aria-selected={active === id}
          className={`tab ${active === id ? "is-active" : ""}`}
          onClick={() => onPick(id)}
        >
          <span className="tab-inner mono">
            {t.tabs[id]}
            {dirty(id) && <span className="tab-dirty" title={t.dirty} aria-label={t.dirty} />}
          </span>
        </button>
      ))}
    </div>
  );
}

export function InfraPage({ bulkBusy, onRunStarted }: { bulkBusy: boolean; onRunStarted: (r: Run) => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const q = useQuery({ queryKey: ["infra-files"], queryFn: api.infraFiles });
  const [active, setActive] = useState<InfraFileId>("compose");
  const [, force] = useState(0);
  const [error, setError] = useState<{ id: InfraFileId; text: string; stale: boolean } | null>(null);
  const [confirmReload, setConfirmReload] = useState(false);
  const [selection, setSelection] = useState<{ start: number; end: number; seq: number } | null>(null);

  const file = q.data?.files.find((f) => f.id === active);

  const draftOf = (f: InfraFile): Draft => {
    const d = drafts.get(f.id);
    if (!d || (d.text === d.base && d.base !== f.content)) {
      const fresh = { base: f.content, text: f.content };
      drafts.set(f.id, fresh);
      return fresh;
    }
    return d;
  };
  const isDirty = (id: InfraFileId) => {
    const d = drafts.get(id);
    return Boolean(d && d.text !== d.base);
  };
  const setText = (id: InfraFileId, text: string) => {
    const d = drafts.get(id);
    if (!d) return;
    drafts.set(id, { ...d, text });
    force((n) => n + 1);
  };
  const anyDirty = ids.some(isDirty);

  useEffect(() => {
    if (!anyDirty) return;
    const onLeave = (e: BeforeUnloadEvent) => {
      e.preventDefault();
    };
    window.addEventListener("beforeunload", onLeave);
    return () => window.removeEventListener("beforeunload", onLeave);
  }, [anyDirty]);

  const save = useMutation({
    mutationFn: ({ f, apply }: { f: InfraFile; apply: boolean }) =>
      api.saveInfraFile(f.id, { content: draftOf(f).text, hash: f.hash, apply }),
    onSuccess: (res, { apply }) => {
      setError(null);
      drafts.set(res.file.id, { base: res.file.content, text: res.file.content });
      qc.setQueryData<InfraFilesView>(["infra-files"], (old) =>
        old ? { files: old.files.map((x) => (x.id === res.file.id ? res.file : x)) } : old,
      );
      void qc.invalidateQueries({ queryKey: ["state"] });
      void qc.invalidateQueries({ queryKey: ["ports"] });
      if (res.applyError) toast({ tone: "danger", message: t.applyFailed, detail: res.applyError });
      else toast({ tone: "success", message: apply && res.run ? t.savedApplied : t.saved, detail: res.file.path });
      if (res.run) onRunStarted(res.run);
    },
    onError: (e, { f }) => setError({ id: f.id, text: errorText(e), stale: isStale(e) }),
  });

  const submit = (apply: boolean) => {
    if (!file || save.isPending) return;
    if (!isDirty(file.id) && !apply) {
      toast({ tone: "success", message: t.noChanges });
      return;
    }
    save.mutate({ f: file, apply });
  };

  const reload = async () => {
    drafts.delete(active);
    setError(null);
    await q.refetch();
    force((n) => n + 1);
  };

  const addSnippet = (id: string) => {
    if (!file) return;
    const s = snippets.find((x) => x.id === id);
    if (!s) return;
    const current = draftOf(file).text;
    const res = insertWithDeps(current, s.id, detectPrefix(current));
    if (res.kind === "exists") {
      toast({ tone: "danger", message: t.snippetExists(s.id) });
      return;
    }
    setText(file.id, res.text);
    setSelection({ start: res.start, end: res.end, seq: Date.now() });
    toast({ tone: "success", message: t.snippetAdded(res.added.join(", ")), detail: t.snippetNotes[s.id] });
  };

  if (!q.data || !file) {
    return (
      <main className="page" id="main">
        <SectionBar title={t.title} jp={t.titleJp} level={1} />
        {q.isError ? (
          <div className="empty">
            <StatusGlyph kind="fault" size={32} />
            <div className="empty-code">{ru.error.title}</div>
            <p className="mono">{errorText(q.error)}</p>
            <Button variant="outline" onClick={() => void q.refetch()}>
              {ru.error.retry}
            </Button>
          </div>
        ) : (
          <div className="skeleton-rows">
            {Array.from({ length: 8 }, (_, i) => (
              <div key={i} className="skeleton-row" />
            ))}
          </div>
        )}
      </main>
    );
  }

  const draft = draftOf(file);
  const dirty = draft.text !== draft.base;
  const fileError = error?.id === file.id ? error : null;
  const busy = save.isPending;

  return (
    <main className="page" id="main">
      <header className="page-head">
        <SectionBar title={t.title} jp={t.titleJp} level={1} count={t.lines(draft.text.split("\n").length)} />
        <p className="page-summary">{t.summary}</p>
        <FileTabs active={active} dirty={isDirty} onPick={setActive} />
      </header>

      <Frame label={t.tabs[file.id] ?? file.id}>
        <div className="infra-editor">
          <p className="meta">
            <CopyText value={file.path} className="mono" /> · {t.hints[file.id]}
          </p>
          {!file.exists && (
            <div className="notice notice-warning" role="status">
              <StatusGlyph kind="triangle" size={12} />
              <span>{t.missing}</span>
            </div>
          )}

          {file.id === "compose" && (
            <details className="infra-snippets" open>
              <summary>
                <span className="field-label">{t.templates}</span>
                <span className="meta micro">{t.templatesCount(snippets.length)}</span>
              </summary>
              <p className="meta micro">{t.templatesNote}</p>
              {snippetGroups.map((g) => (
                <div key={g} className="infra-snippet-group" role="group" aria-label={t.groups[g]}>
                  <span className="infra-snippet-title">{t.groups[g]}</span>
                  <div className="infra-snippet-list">
                    {snippets
                      .filter((s) => s.group === g)
                      .map((s) => {
                        const present = hasService(draft.text, s.id);
                        return (
                          <Button
                            key={s.id}
                            size="sm"
                            variant="ghost"
                            className={present ? "is-present" : ""}
                            icon={<Plus {...iconProps} />}
                            title={present ? t.snippetExists(s.id) : `${t.templatesHint}: ${t.snippetNotes[s.id] ?? ""}`}
                            disabled={busy || present}
                            onClick={() => addSnippet(s.id)}
                          >
                            {s.label}
                          </Button>
                        );
                      })}
                  </div>
                </div>
              ))}
            </details>
          )}

          <CodeEditor
            value={draft.text}
            onChange={(v) => setText(file.id, v)}
            onSave={() => submit(false)}
            disabled={busy}
            label={t.tabs[file.id] ?? file.id}
            selection={selection}
          />

          {fileError && (
            <div className="notice notice-warning infra-error" role="alert">
              <StatusGlyph kind="fault" size={12} />
              <div className="notice-text">
                <div className="notice-title">{fileError.stale ? t.stale : t.invalid}</div>
                {fileError.stale ? <p>{t.staleHint}</p> : <pre className="mono micro">{fileError.text}</pre>}
              </div>
              {fileError.stale && (
                <Button size="sm" variant="outline" icon={<DownloadCloud {...iconProps} />} onClick={() => setConfirmReload(true)}>
                  {t.reload}
                </Button>
              )}
            </div>
          )}

          <p className="meta micro">
            {t.applyNote[file.id]} {file.id === "compose" && t.portsNote}
          </p>
          {file.hasBackup && (
            <p className="meta micro">
              {t.backup} <CopyText value={`${file.path}.sudoku.bak`} className="mono" />
            </p>
          )}

          <div className="config-actions">
            <span className="meta micro infra-shortcut">{dirty ? t.dirty : t.shortcut}</span>
            <Button
              variant="ghost"
              icon={<RotateCcw {...iconProps} />}
              disabled={!dirty || busy}
              onClick={() => {
                setText(file.id, draft.base);
                setError(null);
              }}
            >
              {t.reset}
            </Button>
            <Button
              variant="outline"
              icon={<Save {...iconProps} />}
              loading={busy && !save.variables?.apply}
              loadingLabel={t.saving}
              disabled={!dirty || busy}
              onClick={() => submit(false)}
            >
              {t.save}
            </Button>
            <Button
              variant="primary"
              icon={<Play {...iconProps} />}
              loading={busy && Boolean(save.variables?.apply)}
              loadingLabel={t.saving}
              disabled={busy || bulkBusy}
              disabledReason={bulkBusy ? ru.actions.busy : undefined}
              onClick={() => submit(true)}
            >
              {t.saveApply}
            </Button>
          </div>
        </div>
      </Frame>

      <ConfirmDialog
        open={confirmReload}
        onOpenChange={setConfirmReload}
        danger
        title={t.reload}
        body={<p>{t.staleHint}</p>}
        confirmLabel={t.reload}
        confirmIcon={<DownloadCloud {...iconProps} />}
        onConfirm={() => void reload()}
      />
    </main>
  );
}
