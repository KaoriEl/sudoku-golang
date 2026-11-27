import { Ban, Power } from "lucide-react";
import { Button } from "../../components/ui/Button";
import { ru } from "../../i18n/ru";

const iconProps = { size: 14, strokeWidth: 2, strokeLinecap: "square" as const };

export function DisabledBadge() {
  return (
    <span className="badge badge-neutral badge-disabled" title={ru.disabled.hint}>
      <Ban size={10} strokeWidth={2.5} strokeLinecap="square" aria-hidden />
      {ru.disabled.badge}
    </span>
  );
}

export function ToggleButton({ disabled, loading, onClick }: { disabled: boolean; loading: boolean; onClick: () => void }) {
  return (
    <Button
      size="sm"
      variant="ghost"
      iconOnly
      className={disabled ? "btn-toggle-on" : ""}
      aria-pressed={disabled}
      title={disabled ? ru.disabled.enable : ru.disabled.disable}
      icon={disabled ? <Power {...iconProps} /> : <Ban {...iconProps} />}
      loading={loading}
      onClick={onClick}
    >
      {disabled ? ru.disabled.enable : ru.disabled.disable}
    </Button>
  );
}
