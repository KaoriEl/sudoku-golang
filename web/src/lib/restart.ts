const POLL_MS = 500;
const POLL_LIMIT = 60;

export function sleep(ms: number) {
  return new Promise((r) => setTimeout(r, ms));
}

export async function waitForRestart(): Promise<boolean> {
  await sleep(800);
  for (let i = 0; i < POLL_LIMIT; i++) {
    try {
      const res = await fetch("/api/config", { cache: "no-store" });
      if (res.ok) return true;
    } catch {
      await sleep(0);
    }
    await sleep(POLL_MS);
  }
  return false;
}

export async function waitForOrigin(url: string): Promise<boolean> {
  await sleep(800);
  for (let i = 0; i < POLL_LIMIT; i++) {
    try {
      await fetch(`${url}/api/config`, { mode: "no-cors", cache: "no-store" });
      return true;
    } catch {
      await sleep(0);
    }
    await sleep(POLL_MS);
  }
  return false;
}
