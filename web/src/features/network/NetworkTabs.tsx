import { rf } from "../../i18n/ru.features";
import type { Route } from "../../lib/hooks";

const tabs: { route: Route; label: string }[] = [
  { route: "ports", label: rf.network.tabs.ports },
  { route: "domains", label: rf.network.tabs.domains },
];

export function NetworkTabs({ active, go, href }: { active: Route; go: (r: Route) => void; href: (r: Route) => string }) {
  return (
    <nav className="tabs page-tabs" aria-label={rf.network.title}>
      {tabs.map((t) => (
        <a
          key={t.route}
          href={href(t.route)}
          className={`tab ${active === t.route ? "is-active" : ""}`}
          aria-current={active === t.route ? "page" : undefined}
          onClick={(e) => {
            if (e.metaKey || e.ctrlKey || e.shiftKey) return;
            e.preventDefault();
            go(t.route);
          }}
        >
          <span className="tab-inner">{t.label}</span>
        </a>
      ))}
    </nav>
  );
}
