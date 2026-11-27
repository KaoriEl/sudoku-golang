import * as Dialog from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import type { Run, Step, StepStatus } from "../../api/types";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { SectionBar } from "../../components/ui/Frame";
import { ru } from "../../i18n/ru";
import { runActionLabel, runPhaseLabel } from "../../i18n/ru.features";
import { absolute, duration } from "../../lib/format";
import { useNow } from "../../lib/hooks";

const stepGlyph: Record<StepStatus, { kind: "ring" | "square" | "fault" | "outline" | "dashed"; tone: string }> = {
  pending: { kind: "outline", tone: "neutral" },
  skipped: { kind: "dashed", tone: "neutral" },
  running: { kind: "ring", tone: "progress" },
  success: { kind: "square", tone: "success" },
  failed: { kind: "fault", tone: "danger" },
};

const runTone: Record<string, string> = {
  running: "progress",
  success: "success",
  partial: "warning",
  failed: "danger",
};

function StepItem({ step, now }: { step: Step; now: number }) {
  const g = stepGlyph[step.status];
  const text = [step.output, step.error].filter(Boolean).join("\n");
  return (
    <details className={`step step-${step.status}`} open={step.status === "failed" || step.status === "running"}>
      <summary>
        <span className={`tone-${g.tone} ${g.kind === "ring" ? "spin" : ""}`}>
          <StatusGlyph kind={g.kind} size={12} />
        </span>
        <span className="step-phase mono micro">{runPhaseLabel(step.phase)}</span>
        <span className="step-file mono" title={step.composeFile}>
          {step.rel}
        </span>
        <span className="step-status">{ru.run.step[step.status]}</span>
        <span className="step-time mono micro">{duration(step.startedAt, step.finishedAt, now)}</span>
      </summary>
      {step.command && <code className="step-cmd">{step.command}</code>}
      <pre className="log">{text || ru.run.noOutput}</pre>
    </details>
  );
}

interface Props {
  run: Run | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  workers: number;
}

export function RunDrawer({ run, open, onOpenChange, workers }: Props) {
  const now = useNow(open && run?.status === "running" ? 1000 : 0);
  const steps = run?.steps ?? [];
  const ok = steps.filter((s) => s.status === "success").length;

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange} modal={false}>
      <Dialog.Portal>
        <Dialog.Content className="drawer" onInteractOutside={(e) => e.preventDefault()}>
          <span className="bracket bracket-tl" aria-hidden />
          <span className="bracket bracket-bl" aria-hidden />
          <div className="drawer-head">
            <SectionBar title={ru.run.title} jp={ru.run.titleJp} level={2}>
              <Dialog.Close className="btn btn-ghost btn-sm btn-icon" aria-label={ru.run.close} title={ru.run.close}>
                <X size={16} strokeWidth={2} strokeLinecap="square" />
              </Dialog.Close>
            </SectionBar>
            <Dialog.Title className="visually-hidden">{ru.run.title}</Dialog.Title>
            {run && (
              <Dialog.Description asChild>
                <div className="drawer-meta">
                  <span className={`badge badge-${runTone[run.status]}`}>
                    {run.status === "running" ? (
                      <span className="spin">
                        <StatusGlyph kind="ring" size={10} />
                      </span>
                    ) : (
                      <StatusGlyph kind={run.status === "success" ? "square" : run.status === "failed" ? "fault" : "triangle"} size={10} />
                    )}
                    {ru.run.status[run.status]}
                  </span>
                  <span className="drawer-action">{runActionLabel(run.action)}</span>
                  {run.target && <span className="mono micro">{run.target}</span>}
                  <span className="mono micro" title={absolute(run.startedAt)}>
                    {ru.run.duration(duration(run.startedAt, run.finishedAt, now))}
                  </span>
                  <span className="meta">{ru.run.summary(ok, steps.length)}</span>
                  {(run.action === "stop" || run.action === "restart") && <span className="mono micro meta">{ru.run.parallel(workers)}</span>}
                </div>
              </Dialog.Description>
            )}
          </div>
          <div className="drawer-body">
            {steps.map((s, i) => (
              <StepItem key={`${s.phase}-${s.composeFile}-${i}`} step={s} now={now} />
            ))}
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
