import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { ArrowDown, ChevronDown, ChevronUp, Copy, Download, Eraser, FileDown, Filter, Layers, RefreshCw, Search, WrapText } from "lucide-react";
import { logsDownloadUrl, logsUrl, projectLogsUrl } from "../../api/client";
import type { Container, LogLine } from "../../api/types";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { Button } from "../../components/ui/Button";
import { ru } from "../../i18n/ru";
import { rf } from "../../i18n/ru.features";

const MAX_LINES = 5000;
const TAILS = [200, 500, 2000, 10000];
const iconProps = { size: 14, strokeWidth: 2, strokeLinecap: "square" as const };

type Level = "error" | "warn" | "debug" | "";
type LevelFilter = "all" | "warn" | "error";

interface Line extends LogLine {
  n: number;
  lv: Level;
}

type ConnState = "connecting" | "live" | "ended" | "error";

const dangerRe = /\b(error|err|fatal|panic|critical|exception)\b/i;
const warnRe = /\b(warn|warning)\b/i;
const jsonSeverityRe = /"(?:severity|level)"\s*:\s*"([a-z]+)"/i;

function levelOf(m: string): Level {
  const sev = jsonSeverityRe.exec(m)?.[1]?.toLowerCase();
  if (sev) {
    if (sev.startsWith("err") || sev === "fatal" || sev === "panic" || sev === "critical") return "error";
    if (sev.startsWith("warn")) return "warn";
    if (sev === "debug" || sev === "trace") return "debug";
    return "";
  }
  if (dangerRe.test(m)) return "error";
  if (warnRe.test(m)) return "warn";
  return "";
}

const toneOf: Record<Level, string> = { error: "danger", warn: "warning", debug: "subtle", "": "none" };

function passes(l: Line, f: LevelFilter): boolean {
  if (f === "error") return l.lv === "error";
  if (f === "warn") return l.lv === "error" || l.lv === "warn";
  return true;
}

function shortTime(t: string): string {
  const i = t.indexOf("T");
  return i >= 0 ? t.slice(i + 1, i + 13) : t;
}

function highlight(text: string, q: string): ReactNode {
  if (!q) return text;
  const lower = text.toLowerCase();
  const parts: ReactNode[] = [];
  let from = 0;
  for (let i = lower.indexOf(q); i >= 0; i = lower.indexOf(q, i + q.length)) {
    if (i > from) parts.push(text.slice(from, i));
    parts.push(<mark key={i}>{text.slice(i, i + q.length)}</mark>);
    from = i + q.length;
  }
  if (from < text.length) parts.push(text.slice(from));
  return parts;
}

export function LogViewer({ container }: { container: Container }) {
  const [project, setProject] = useState(false);
  const [lines, setLines] = useState<Line[]>([]);
  const [state, setState] = useState<ConnState>("connecting");
  const [tailN, setTailN] = useState(500);
  const [query, setQuery] = useState("");
  const [onlyMatches, setOnlyMatches] = useState(false);
  const [level, setLevel] = useState<LevelFilter>("all");
  const [source, setSource] = useState("");
  const [wrap, setWrap] = useState(true);
  const [follow, setFollow] = useState(true);
  const [unseen, setUnseen] = useState(0);
  const [session, setSession] = useState(0);
  const [capped, setCapped] = useState(false);
  const [cursor, setCursor] = useState(0);
  const scroller = useRef<HTMLDivElement>(null);
  const counter = useRef(0);
  const followRef = useRef(follow);
  followRef.current = follow;
  const canProject = container.managed && container.composeFile !== "";

  useEffect(() => {
    setLines([]);
    setCapped(false);
    setUnseen(0);
    setSource("");
    setState("connecting");
    counter.current = 0;
    const url = project ? projectLogsUrl(container.composeFile, [], tailN) : logsUrl(container.id, tailN);
    const es = new EventSource(url);
    es.addEventListener("meta", () => setState("live"));
    es.addEventListener("lines", (e) => {
      const batch = JSON.parse((e as MessageEvent<string>).data) as LogLine[];
      const mapped = batch.map((l) => ({ ...l, n: ++counter.current, lv: levelOf(l.m) }));
      setLines((prev) => {
        const next = prev.concat(mapped);
        if (next.length > MAX_LINES) {
          setCapped(true);
          return next.slice(next.length - MAX_LINES);
        }
        return next;
      });
      if (!followRef.current) setUnseen((u) => u + mapped.length);
    });
    es.addEventListener("end", () => {
      setState("ended");
      es.close();
    });
    es.onerror = () => {
      setState((s) => (s === "ended" ? s : "error"));
      es.close();
    };
    return () => es.close();
  }, [container.id, container.composeFile, container.startedAt, tailN, session, project]);

  const q = query.trim().toLowerCase();
  const sources = useMemo(() => (project ? [...new Set(lines.map((l) => l.c ?? ""))].filter(Boolean).sort() : []), [lines, project]);
  const filtered = useMemo(
    () => lines.filter((l) => passes(l, level) && (!source || l.c === source)),
    [lines, level, source],
  );
  const matches = useMemo(() => (q ? filtered.filter((l) => l.m.toLowerCase().includes(q)).map((l) => l.n) : []), [filtered, q]);
  const visible = useMemo(() => {
    if (!onlyMatches || !q) return filtered;
    const set = new Set(matches);
    return filtered.filter((l) => set.has(l.n));
  }, [filtered, matches, onlyMatches, q]);

  useEffect(() => setCursor(0), [q]);

  useEffect(() => {
    if (follow && !q && scroller.current) scroller.current.scrollTop = scroller.current.scrollHeight;
  }, [visible, follow, q]);

  const jump = (dir: 1 | -1) => {
    if (matches.length === 0) return;
    const next = (cursor + dir + matches.length) % matches.length;
    setCursor(next);
    setFollow(false);
    document.getElementById(`logline-${container.id}-${matches[next]}`)?.scrollIntoView({ block: "center" });
  };

  const onScroll = useCallback(() => {
    const el = scroller.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
    if (atBottom !== followRef.current) {
      setFollow(atBottom);
      if (atBottom) setUnseen(0);
    }
  }, []);

  const text = () => visible.map((l) => `${l.t} ${l.c ? `[${l.c}] ` : ""}${l.s === "err" ? "[stderr] " : ""}${l.m}`).join("\n");

  const downloadVisible = () => {
    const blob = new Blob([text()], { type: "text/plain;charset=utf-8" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `${container.name}-visible.log`;
    a.click();
    URL.revokeObjectURL(a.href);
  };

  const stateBadge = {
    connecting: { tone: "progress", label: ru.logs.connecting, glyph: "ring" as const },
    live: { tone: "success", label: ru.logs.live, glyph: "square" as const },
    ended: { tone: "neutral", label: ru.logs.ended, glyph: "outline" as const },
    error: { tone: "danger", label: ru.logs.error, glyph: "fault" as const },
  }[state];
  const current = matches[cursor];

  return (
    <div className="logview">
      <div className="logview-bar">
        <span className={`badge badge-${stateBadge.tone}`}>
          <span className={stateBadge.glyph === "ring" ? "spin" : ""}>
            <StatusGlyph kind={stateBadge.glyph} size={10} />
          </span>
          {stateBadge.label}
        </span>
        {(state === "ended" || state === "error") && (
          <Button size="sm" variant="outline" icon={<RefreshCw {...iconProps} />} onClick={() => setSession((n) => n + 1)}>
            {ru.logs.reconnect}
          </Button>
        )}
        {canProject && (
          <Button
            size="sm"
            variant={project ? "outline" : "ghost"}
            aria-pressed={project}
            icon={<Layers {...iconProps} />}
            title={rf.logs.projectHint}
            onClick={() => setProject((p) => !p)}
          >
            {project ? rf.logs.project : rf.logs.single}
          </Button>
        )}
        <label className="search search-sm">
          <Search {...iconProps} aria-hidden />
          <span className="visually-hidden">{ru.logs.filter}</span>
          <input
            type="search"
            value={query}
            placeholder={ru.logs.filter}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                jump(e.shiftKey ? -1 : 1);
              }
            }}
          />
        </label>
        {q && (
          <span className="logview-matches mono micro" aria-live="polite">
            {rf.logs.matches(matches.length ? cursor + 1 : 0, matches.length)}
            <Button size="sm" variant="ghost" iconOnly icon={<ChevronUp {...iconProps} />} title={rf.logs.prev} onClick={() => jump(-1)}>
              {rf.logs.prev}
            </Button>
            <Button size="sm" variant="ghost" iconOnly icon={<ChevronDown {...iconProps} />} title={rf.logs.next} onClick={() => jump(1)}>
              {rf.logs.next}
            </Button>
            <Button size="sm" variant={onlyMatches ? "outline" : "ghost"} iconOnly aria-pressed={onlyMatches} icon={<Filter {...iconProps} />} title={rf.logs.onlyMatches} onClick={() => setOnlyMatches((v) => !v)}>
              {rf.logs.onlyMatches}
            </Button>
          </span>
        )}
        <label className="select">
          <span className="visually-hidden">{rf.logs.level}</span>
          <select value={level} onChange={(e) => setLevel(e.target.value as LevelFilter)} title={rf.logs.level}>
            {(["all", "warn", "error"] as const).map((l) => (
              <option key={l} value={l}>
                {rf.logs.levels[l]}
              </option>
            ))}
          </select>
        </label>
        {project && sources.length > 1 && (
          <label className="select">
            <span className="visually-hidden">{rf.logs.source}</span>
            <select value={source} onChange={(e) => setSource(e.target.value)} title={rf.logs.source}>
              <option value="">{rf.logs.allSources}</option>
              {sources.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </label>
        )}
        <label className="select">
          <span className="visually-hidden">{ru.logs.tail}</span>
          <select value={tailN} onChange={(e) => setTailN(Number(e.target.value))} title={ru.logs.tail}>
            {TAILS.map((t) => (
              <option key={t} value={t}>
                tail {t}
              </option>
            ))}
          </select>
        </label>
        <div className="logview-tools">
          <Button size="sm" variant={follow ? "outline" : "ghost"} aria-pressed={follow} title={ru.logs.followHint} onClick={() => setFollow((f) => !f)}>
            {ru.logs.follow} {follow ? "●" : "○"}
          </Button>
          <Button size="sm" variant="ghost" iconOnly aria-pressed={wrap} icon={<WrapText {...iconProps} />} title={ru.logs.wrap} onClick={() => setWrap((w) => !w)}>
            {ru.logs.wrap}
          </Button>
          <Button size="sm" variant="ghost" iconOnly icon={<Eraser {...iconProps} />} title={ru.logs.clear} onClick={() => setLines([])}>
            {ru.logs.clear}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            iconOnly
            icon={<Copy {...iconProps} />}
            title={ru.logs.copy}
            onClick={() => void navigator.clipboard.writeText(text()).catch(() => undefined)}
          >
            {ru.logs.copy}
          </Button>
          <Button size="sm" variant="ghost" iconOnly icon={<Download {...iconProps} />} title={rf.logs.downloadVisible} onClick={downloadVisible}>
            {rf.logs.downloadVisible}
          </Button>
          <a className="btn btn-ghost btn-sm btn-icon" href={logsDownloadUrl(container.id, true)} download title={rf.logs.downloadAll}>
            <FileDown {...iconProps} aria-hidden />
            <span className="visually-hidden">{rf.logs.downloadAll}</span>
          </a>
        </div>
      </div>
      <div className="logview-meta mono micro">
        {ru.logs.count(visible.length, lines.length)}
        {capped && ` · ${ru.logs.capped(MAX_LINES)}`}
      </div>
      <div ref={scroller} className={`logview-body ${wrap ? "is-wrap" : ""}`} onScroll={onScroll} role="log" aria-live="off" tabIndex={0}>
        {visible.length === 0 && state !== "connecting" && <div className="logview-empty">{q || level !== "all" ? ru.logs.noMatch : ru.logs.empty}</div>}
        {visible.map((l) => (
          <div
            key={l.n}
            id={`logline-${container.id}-${l.n}`}
            className={`logline ${l.s === "err" ? "is-err" : ""} tone-${toneOf[l.lv]} ${l.n === current ? "is-current" : ""}`}
          >
            <span className="logline-time" title={l.t}>
              {shortTime(l.t)}
            </span>
            {l.c && <span className="logline-src mono">{l.c}</span>}
            <span className="logline-msg">{q ? highlight(l.m, q) : l.m}</span>
          </div>
        ))}
      </div>
      {!follow && unseen > 0 && (
        <button
          type="button"
          className="logview-jump"
          onClick={() => {
            setFollow(true);
            setUnseen(0);
          }}
        >
          <ArrowDown {...iconProps} aria-hidden />
          {ru.logs.newLines(unseen)}
        </button>
      )}
    </div>
  );
}
