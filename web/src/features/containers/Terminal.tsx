import { useEffect, useRef, useState } from "react";
import { Terminal as XTerm } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { RefreshCw } from "lucide-react";
import { execUrl } from "../../api/client";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { Button } from "../../components/ui/Button";
import { ru } from "../../i18n/ru";

type ConnState = "connecting" | "open" | "closed";

function cssVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function theme() {
  return {
    background: cssVar("--surface-sunken"),
    foreground: cssVar("--text"),
    cursor: cssVar("--accent"),
    cursorAccent: cssVar("--surface-sunken"),
    selectionBackground: cssVar("--accent-soft"),
    black: cssVar("--surface-hover"),
    red: cssVar("--danger"),
    green: cssVar("--success"),
    yellow: cssVar("--warning"),
    blue: cssVar("--progress"),
    magenta: cssVar("--accent-text"),
    cyan: cssVar("--progress"),
    white: cssVar("--text"),
    brightBlack: cssVar("--text-subtle"),
    brightRed: cssVar("--danger"),
    brightGreen: cssVar("--success"),
    brightYellow: cssVar("--warning"),
    brightBlue: cssVar("--progress"),
    brightMagenta: cssVar("--accent"),
    brightCyan: cssVar("--progress"),
    brightWhite: cssVar("--text"),
  };
}

export function Terminal({ containerId, themeKey }: { containerId: string; themeKey: string }) {
  const host = useRef<HTMLDivElement>(null);
  const [state, setState] = useState<ConnState>("connecting");
  const [reason, setReason] = useState("");
  const [session, setSession] = useState(0);

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    let disposed = false;
    const term = new XTerm({
      fontFamily: '"JetBrains Mono", ui-monospace, monospace',
      fontSize: 13,
      lineHeight: 1.2,
      cursorBlink: true,
      cursorStyle: "block",
      scrollback: 5000,
      theme: theme(),
      allowProposedApi: false,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);

    let ws: WebSocket | null = null;
    const encoder = new TextEncoder();
    const sendResize = () => {
      if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
    };

    const start = async () => {
      try {
        await document.fonts.load('13px "JetBrains Mono"');
      } catch {
        void 0;
      }
      if (disposed) return;
      term.open(el);
      fit.fit();
      setState("connecting");
      setReason("");
      ws = new WebSocket(execUrl(containerId, term.cols, term.rows));
      ws.binaryType = "arraybuffer";
      ws.onopen = () => {
        setState("open");
        term.focus();
        sendResize();
      };
      ws.onmessage = (e) => {
        if (e.data instanceof ArrayBuffer) term.write(new Uint8Array(e.data));
        else term.write(String(e.data));
      };
      ws.onclose = (e) => {
        setState("closed");
        setReason(e.reason);
        term.write(`\r\n\x1b[2m— ${ru.term.closed}${e.reason ? `: ${e.reason}` : ""} —\x1b[0m\r\n`);
      };
      term.onData((d) => {
        if (ws?.readyState === WebSocket.OPEN) ws.send(encoder.encode(d));
      });
      term.onResize(sendResize);
    };
    void start();

    const ro = new ResizeObserver(() => {
      try {
        fit.fit();
      } catch {
        void 0;
      }
    });
    ro.observe(el);

    return () => {
      disposed = true;
      ro.disconnect();
      ws?.close();
      term.dispose();
    };
  }, [containerId, session, themeKey]);

  return (
    <div className="termview">
      <div className="logview-bar">
        <span className={`badge badge-${state === "open" ? "success" : state === "connecting" ? "progress" : "neutral"}`}>
          <span className={state === "connecting" ? "spin" : ""}>
            <StatusGlyph kind={state === "open" ? "square" : state === "connecting" ? "ring" : "outline"} size={10} />
          </span>
          {state === "open" ? ru.term.connected : state === "connecting" ? ru.term.connecting : ru.term.closed}
        </span>
        {state === "closed" && (
          <Button size="sm" variant="outline" icon={<RefreshCw size={14} strokeWidth={2} strokeLinecap="square" />} onClick={() => setSession((n) => n + 1)}>
            {ru.term.reconnect}
          </Button>
        )}
        <span className="meta termview-hint">{state === "closed" && reason ? reason : ru.term.hint}</span>
      </div>
      <div ref={host} className="termview-body" />
    </div>
  );
}
