import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { EVENTS_STREAM } from "../api/client";
import type { Alert, Run, Snapshot } from "../api/types";

export type EventsState = "connecting" | "live" | "down";

const RETRY_MS = 3000;

export function useServerEvents(onAlert: (a: Alert) => void): EventsState {
  const qc = useQueryClient();
  const [state, setState] = useState<EventsState>("connecting");
  const alertRef = useRef(onAlert);
  alertRef.current = onAlert;

  useEffect(() => {
    let es: EventSource | null = null;
    let retry: number | null = null;
    let closed = false;

    const open = () => {
      es = new EventSource(EVENTS_STREAM);
      es.addEventListener("state", (e) => {
        const snap = JSON.parse((e as MessageEvent<string>).data) as Snapshot;
        qc.setQueryData(["state"], snap);
        setState("live");
      });
      es.addEventListener("run", (e) => {
        const run = JSON.parse((e as MessageEvent<string>).data) as Run;
        qc.setQueryData(["run", run.id], run);
        if (run.status !== "running") void qc.invalidateQueries({ queryKey: ["runs"] });
      });
      es.addEventListener("alert", (e) => {
        const a = JSON.parse((e as MessageEvent<string>).data) as Alert;
        qc.setQueryData<Alert[]>(["alerts"], (old) => [a, ...(old ?? [])].slice(0, 50));
        alertRef.current(a);
      });
      es.onerror = () => {
        setState("down");
        es?.close();
        es = null;
        if (!closed && retry === null) {
          retry = window.setTimeout(() => {
            retry = null;
            if (!closed) open();
          }, RETRY_MS);
        }
      };
    };
    open();
    return () => {
      closed = true;
      es?.close();
      if (retry !== null) window.clearTimeout(retry);
    };
  }, [qc]);

  return state;
}

export function notifyBrowser(title: string, body: string): void {
  if (typeof Notification === "undefined" || Notification.permission !== "granted" || !document.hidden) return;
  try {
    new Notification(title, { body, tag: `${title}:${body}`, icon: "/favicon.svg" });
  } catch {
    return;
  }
}
