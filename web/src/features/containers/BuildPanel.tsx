import { useQuery } from "@tanstack/react-query";
import { Hammer } from "lucide-react";
import { ApiError, api } from "../../api/client";
import type { Container } from "../../api/types";
import { StatusGlyph } from "../../components/hud/StatusGlyph";
import { Button } from "../../components/ui/Button";
import { CopyText } from "../../components/ui/CopyButton";
import { SectionBar } from "../../components/ui/Frame";
import { ru } from "../../i18n/ru";

const iconProps = { size: 14, strokeWidth: 2, strokeLinecap: "square" as const };

interface Props {
  container: Container;
  bulkBusy: boolean;
  rebuilding: boolean;
  onRebuild: (noCache: boolean) => void;
}

function CommandLine({ label, command }: { label: string; command: string }) {
  return (
    <div className="cmd">
      <span className="cmd-label mono micro">{label}</span>
      <CopyText value={command} className="cmd-text mono">
        {command}
      </CopyText>
    </div>
  );
}

export function BuildPanel({ container: c, bulkBusy, rebuilding, onRebuild }: Props) {
  const q = useQuery({ queryKey: ["commands", c.id], queryFn: () => api.commands(c.id), staleTime: 30_000 });
  const reason = !c.managed ? ru.build.foreign : bulkBusy ? ru.actions.busy : undefined;
  const data = q.data;
  const build = data?.build;

  return (
    <div className="buildpanel">
      <div className="buildpanel-actions">
        <Button
          variant="primary"
          icon={<Hammer {...iconProps} />}
          loading={rebuilding}
          loadingLabel={ru.build.rebuilding}
          disabled={Boolean(reason)}
          disabledReason={reason}
          onClick={() => onRebuild(false)}
        >
          {ru.build.rebuild}
        </Button>
        <Button variant="outline" disabled={Boolean(reason) || rebuilding} disabledReason={reason} onClick={() => onRebuild(true)}>
          {ru.build.rebuildNoCache}
        </Button>
        <span className="meta">{ru.build.rebuildHint}</span>
      </div>

      {q.isLoading && (
        <p className="meta">
          <span className="spin">
            <StatusGlyph kind="ring" size={10} />
          </span>{" "}
          {ru.build.loading}
        </p>
      )}
      {q.isError && (
        <p className="tone-danger mono micro">{q.error instanceof ApiError ? q.error.message : String(q.error)}</p>
      )}

      {data && (
        <>
          <SectionBar title={ru.build.dockerBuild} />
          {build ? (
            <div className="build-full">
              {build.hasBuild ? <p className="meta">{ru.build.dockerBuildHint}</p> : <p className="meta">{ru.build.noBuild}</p>}
              {build.inline && <p className="tone-warning meta">{ru.build.inline}</p>}
              <CopyText value={build.command} className="cmd-block mono">
                {build.command}
              </CopyText>
              {build.hasBuild && (
                <dl className="kv">
                  <dt>{ru.build.image}</dt>
                  <dd className="mono">{build.image}</dd>
                  <dt>{ru.build.context}</dt>
                  <dd className="mono">{build.context}</dd>
                  <dt>{ru.build.dockerfile}</dt>
                  <dd className="mono">{build.dockerfile}</dd>
                  {build.target && (
                    <>
                      <dt>{ru.build.target}</dt>
                      <dd className="mono">{build.target}</dd>
                    </>
                  )}
                  {build.args && Object.keys(build.args).length > 0 && (
                    <>
                      <dt>{ru.build.args}</dt>
                      <dd className="mono">
                        {Object.entries(build.args).map(([k, v]) => (
                          <div key={k}>
                            {k}={v}
                          </div>
                        ))}
                      </dd>
                    </>
                  )}
                </dl>
              )}
            </div>
          ) : (
            <p className="tone-danger mono micro">{data.buildError}</p>
          )}

          <SectionBar title={ru.build.title} jp={ru.build.titleJp} />
          <div className="cmds">
            {data.commands.map((cmd) => (
              <CommandLine key={cmd.id} label={cmd.label} command={cmd.command} />
            ))}
          </div>
        </>
      )}
    </div>
  );
}
