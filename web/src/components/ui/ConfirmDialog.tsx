import * as Dialog from "@radix-ui/react-dialog";
import { useEffect, useState, type ReactNode } from "react";
import { ru } from "../../i18n/ru";
import { Button } from "./Button";
import { Checkbox } from "./Checkbox";
import { StatusGlyph } from "../hud/StatusGlyph";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  body: ReactNode;
  extra?: ReactNode;
  confirmLabel: string;
  confirmIcon?: ReactNode;
  cli?: string;
  danger?: boolean;
  offerDontAsk?: boolean;
  onConfirm: (dontAskAgain: boolean) => void;
}

export function ConfirmDialog({ open, onOpenChange, title, body, extra, confirmLabel, confirmIcon, cli, danger, offerDontAsk, onConfirm }: Props) {
  const [dontAsk, setDontAsk] = useState(false);

  useEffect(() => {
    if (open) setDontAsk(false);
  }, [open]);

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="scrim" />
        <Dialog.Content className={`dialog ${danger ? "dialog-danger" : ""}`}>
          {danger && <div className="hazard-band" aria-hidden />}
          <span className="bracket bracket-tr" aria-hidden />
          <span className="bracket bracket-bl" aria-hidden />
          <div className="dialog-inner">
            <Dialog.Title className="dialog-title">
              {danger && <StatusGlyph kind="fault" size={14} />}
              {title}
            </Dialog.Title>
            <Dialog.Description asChild>
              <div className="dialog-body">{body}</div>
            </Dialog.Description>
            {extra}
            {cli && (
              <p className="dialog-cli">
                <span>{ru.confirm.cliEquivalent}</span> <code>{cli}</code>
              </p>
            )}
            <div className="dialog-actions">
              {offerDontAsk && (
                <Checkbox className="dialog-dontask" checked={dontAsk} onChange={setDontAsk}>
                  {ru.dontAsk}
                </Checkbox>
              )}
              <Dialog.Close asChild>
                <Button variant="outline" size="md" autoFocus>
                  {ru.confirm.cancel}
                </Button>
              </Dialog.Close>
              <Button
                variant={danger ? "danger" : "primary"}
                icon={confirmIcon}
                onClick={() => {
                  onConfirm(dontAsk);
                  onOpenChange(false);
                }}
              >
                {confirmLabel}
              </Button>
            </div>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
