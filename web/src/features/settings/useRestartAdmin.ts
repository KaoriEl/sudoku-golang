import { useCallback, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError, api } from "../../api/client";
import type { Settings } from "../../api/types";
import { useToast } from "../../components/ui/Toasts";
import { rf } from "../../i18n/ru.features";
import { sleep, waitForRestart } from "../../lib/restart";

const t = rf.prefs;
const RELOAD_DELAY_MS = 1200;

export function useRestartAdmin(): { busy: boolean; restart: () => void } {
  const qc = useQueryClient();
  const toast = useToast();
  const [busy, setBusyState] = useState(false);
  const busyRef = useRef(false);
  const setBusy = (v: boolean) => {
    busyRef.current = v;
    setBusyState(v);
  };

  const run = async () => {
    setBusy(true);
    const before = (qc.getQueryData<Settings>(["settings"]) ?? (await api.settings().catch(() => null)))?.version.version;
    try {
      await api.restartAdmin();
    } catch (e) {
      toast({ tone: "danger", message: t.restartFailed, detail: e instanceof ApiError ? e.message : String(e) });
      setBusy(false);
      return;
    }
    if (!(await waitForRestart())) {
      toast({ tone: "danger", message: t.restartLost });
      setBusy(false);
      return;
    }
    const after = (await api.settings().catch(() => null))?.version.version;
    if (before && after && before !== after) {
      toast({ tone: "success", message: t.restartDone, detail: t.restartUpdated(before, after) });
      await sleep(RELOAD_DELAY_MS);
      window.location.reload();
      return;
    }
    await qc.invalidateQueries();
    toast({ tone: "success", message: t.restartDone, detail: after });
    setBusy(false);
  };

  const runRef = useRef(run);
  runRef.current = run;
  const restart = useCallback(() => {
    if (!busyRef.current) void runRef.current();
  }, []);

  return { busy, restart };
}
