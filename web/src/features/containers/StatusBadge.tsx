import type { Container } from "../../api/types";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { ru } from "../../i18n/ru";
import { statusTone } from "../../lib/format";

export function StatusBadge({ container, pending }: { container: Container; pending?: boolean }) {
  if (pending) {
    return (
      <span className="badge badge-progress">
        <span className="spin">
          <StatusGlyph kind="ring" size={10} />
        </span>
        {ru.pending}
      </span>
    );
  }

  const tone = statusTone[container.status];
  const label = ru.status[container.status];
  const showCode = container.status === "error" && container.exitCode !== 0;
  const tip = [container.state, container.health, container.error].filter(Boolean).join(" · ");

  return (
    <span className={`badge badge-${tone}`} title={tip}>
      <StatusGlyph status={container.status} size={10} />
      {label}
      {showCode && <span className="badge-code mono">{ru.exitCode(container.exitCode)}</span>}
    </span>
  );
}
