import { describe, expect, it } from "vitest";
import { applyAgentEvent, emptyAgentState } from "./agent";

describe("Agent event state", () => {
  it("builds timeline and recommendations from SSE snapshots", () => {
    let state = emptyAgentState("预算 5000 买电脑");
    state = applyAgentEvent(state, { type: "run_snapshot", data: { run: { run_id: "r1", status: "RUNNING" } } });
    state = applyAgentEvent(state, {
      type: "step_snapshot",
      data: { step: { step_no: 1, tool_name: "search_products", status: "SUCCEEDED" } },
    });
    state = applyAgentEvent(state, {
      type: "recommendation_snapshot",
      data: { recommendation: { sku_id: 10, rank_no: 1, validation_status: "VERIFIED" } },
    });

    expect(state.steps).toHaveLength(1);
    expect(state.recommendations).toHaveLength(1);
  });

  it("keeps prompt and stable error code for failed runs", () => {
    const state = applyAgentEvent(emptyAgentState("买手机"), {
      type: "run_failed",
      data: { code: "AGENT_RUN_FAILED" },
    });

    expect(state.prompt).toBe("买手机");
    expect(state.errorCode).toBe("AGENT_RUN_FAILED");
  });
});
