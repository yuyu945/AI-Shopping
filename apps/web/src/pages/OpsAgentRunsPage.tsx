import { useEffect, useState } from "react";
import { api, type OpsAgentRun, type OpsAgentStep, type Recommendation } from "../api/client";

type OpsAgentAPI = Pick<typeof api, "listAgentRunsOps" | "getAgentRunOps">;

export default function OpsAgentRunsPage({ api: client = api }: { api?: OpsAgentAPI }) {
  const [runs, setRuns] = useState<OpsAgentRun[]>([]);
  const [status, setStatus] = useState("ALL");
  const [selected, setSelected] = useState<OpsAgentRun | null>(null);
  const [steps, setSteps] = useState<OpsAgentStep[]>([]);
  const [recommendations, setRecommendations] = useState<Recommendation[]>([]);

  useEffect(() => {
    void load();
  }, [status]);

  async function load() {
    const result = await client.listAgentRunsOps(status === "ALL" ? {} : { status });
    setRuns(result.runs);
  }

  async function open(runID: string) {
    const result = await client.getAgentRunOps(runID);
    setSelected(result.run);
    setSteps(result.steps);
    setRecommendations(result.recommendations);
  }

  return (
    <section className="pageStack">
      <section className="statusBanner compactBanner">
        <div>
          <p className="eyebrow">Operations</p>
          <h1>Agent Run 时间线</h1>
          <p>按状态扫描 Run，查看 trace_id、Step 耗时和脱敏详情。</p>
        </div>
      </section>
      <div className="opsLayout">
        <aside className="sidePanel">
          <div className="panelHeader">
            <h2>Runs</h2>
            <select aria-label="Run status" value={status} onChange={(event) => setStatus(event.target.value)}>
              <option>ALL</option>
              <option>RUNNING</option>
              <option>SUCCEEDED</option>
              <option>FAILED</option>
              <option>TIMEOUT</option>
            </select>
          </div>
          <div className="timeline">
            {runs.map((run) => (
              <button className="opsListButton" key={run.run_id} aria-label={run.run_id} onClick={() => void open(run.run_id)}>
                <strong>{run.run_id}</strong>
                <span>{run.trace_id || "-"}</span>
                <em>{run.status}</em>
              </button>
            ))}
          </div>
        </aside>
        <section className="detailMain">
          {selected ? (
            <>
              <div className="panelHeader">
                <h2>{selected.run_id}</h2>
                <span className="stockBadge">{selected.status}</span>
              </div>
              <dl className="snapshotList">
                <div><dt>Trace</dt><dd>{selected.trace_id}</dd></div>
                <div><dt>Model</dt><dd>{selected.model_name || "-"}</dd></div>
                <div><dt>Prompt</dt><dd>{selected.prompt_version || "-"}</dd></div>
              </dl>
              <div className="timeline">
                {steps.map((step) => (
                  <article className="timelineItem opsStep" key={`${step.step_no}-${step.attempt ?? 1}`}>
                    <span>{step.step_no}</span>
                    <div>
                      <strong>{step.tool_name || step.step_type}</strong>
                      <small>{step.latency_ms ?? 0}ms · attempt {step.attempt ?? 1}{step.error_code ? ` · ${step.error_code}` : ""}</small>
                      <pre>{JSON.stringify({ input: step.input_json ?? {}, output: step.output_json ?? {} }, null, 2)}</pre>
                    </div>
                    <em>{step.status}</em>
                  </article>
                ))}
              </div>
              {recommendations.length ? <p className="muted">{recommendations.length} verified recommendations</p> : null}
            </>
          ) : (
            <div className="statePanel">选择一个 Run 查看时间线。</div>
          )}
        </section>
      </div>
    </section>
  );
}
