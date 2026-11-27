import { useCallback, useEffect, useState } from "react";

export function useNow(intervalMs = 1000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (intervalMs <= 0) return;
    setNow(Date.now());
    const t = window.setInterval(() => setNow(Date.now()), intervalMs);
    return () => window.clearInterval(t);
  }, [intervalMs]);
  return now;
}

type Theme = "dark" | "light";

function currentTheme(): Theme {
  const set = document.documentElement.dataset.theme;
  if (set === "dark" || set === "light") return set;
  return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
}

export function useTheme(): [Theme, () => void] {
  const [theme, setTheme] = useState<Theme>(currentTheme);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);

  const toggle = useCallback(() => {
    setTheme((t) => {
      const nextTheme = t === "dark" ? "light" : "dark";
      try {
        localStorage.setItem("sudoku-theme", nextTheme);
      } catch {
        return nextTheme;
      }
      return nextTheme;
    });
  }, []);

  return [theme, toggle];
}

export function useSearchParam(key: string, fallback: string): [string, (v: string) => void] {
  const read = () => new URLSearchParams(window.location.search).get(key) ?? fallback;
  const [value, setValue] = useState(read);

  useEffect(() => {
    const onPop = () => setValue(read());
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  });

  const update = useCallback(
    (v: string) => {
      setValue(v);
      const params = new URLSearchParams(window.location.search);
      if (v === fallback || v === "") params.delete(key);
      else params.set(key, v);
      const qs = params.toString();
      window.history.replaceState(null, "", `${window.location.pathname}${qs ? `?${qs}` : ""}`);
    },
    [key, fallback],
  );

  return [value, update];
}

const PREF_EVENT = "sudoku-pref";

function readPref(key: string): string | null {
  try {
    return localStorage.getItem(`sudoku.${key}`);
  } catch {
    return null;
  }
}

export function usePref(key: string): [boolean, (v: boolean) => void] {
  const [value, setValue] = useState(() => readPref(key) === "1");

  useEffect(() => {
    const sync = () => setValue(readPref(key) === "1");
    window.addEventListener("storage", sync);
    window.addEventListener(PREF_EVENT, sync);
    return () => {
      window.removeEventListener("storage", sync);
      window.removeEventListener(PREF_EVENT, sync);
    };
  }, [key]);

  const update = useCallback(
    (v: boolean) => {
      try {
        if (v) localStorage.setItem(`sudoku.${key}`, "1");
        else localStorage.removeItem(`sudoku.${key}`);
      } catch {
        setValue(v);
        return;
      }
      window.dispatchEvent(new Event(PREF_EVENT));
    },
    [key],
  );

  return [value, update];
}

export type Route = "containers" | "monitoring" | "runs" | "backups" | "cleanup" | "ports" | "domains" | "settings";

const routePaths: Record<Route, string> = {
  containers: "/",
  monitoring: "/monitoring",
  runs: "/runs",
  backups: "/backups",
  cleanup: "/cleanup",
  ports: "/ports",
  domains: "/domains",
  settings: "/settings",
};

function readRoute(): Route {
  const p = window.location.pathname.replace(/\/+$/, "");
  const found = (Object.keys(routePaths) as Route[]).find((r) => routePaths[r] === p && r !== "containers");
  return found ?? "containers";
}

export function useRoute(): [Route, (r: Route) => void, (r: Route) => string] {
  const [route, setRoute] = useState<Route>(readRoute);

  useEffect(() => {
    const onPop = () => setRoute(readRoute());
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);

  const go = useCallback((r: Route) => {
    if (readRoute() !== r) window.history.pushState(null, "", routePaths[r]);
    setRoute(r);
    window.scrollTo(0, 0);
  }, []);

  return [route, go, (r) => routePaths[r]];
}
