import type { AgentRun, AgentStep, Recommendation, StableErrorCode } from "../api/client";

export type AgentViewState = {
  prompt: string;
  run?: AgentRun;
  steps: AgentStep[];
  recommendations: Recommendation[];
  completed: boolean;
  errorCode?: StableErrorCode;
};

export type AgentViewEvent = {
  type: string;
  data: unknown;
};

export function emptyAgentState(prompt: string): AgentViewState {
  return { prompt, steps: [], recommendations: [], completed: false };
}

export function applyAgentEvent(state: AgentViewState, event: AgentViewEvent): AgentViewState {
  switch (event.type) {
    case "run_snapshot": {
      const run = readField<AgentRun>(event.data, "run");
      return run ? { ...state, run } : state;
    }
    case "step_snapshot": {
      const step = readField<AgentStep>(event.data, "step");
      return step ? { ...state, steps: upsertStep(state.steps, step) } : state;
    }
    case "recommendation_snapshot": {
      const recommendation = readField<Recommendation>(event.data, "recommendation");
      return recommendation
        ? { ...state, recommendations: upsertRecommendation(state.recommendations, recommendation) }
        : state;
    }
    case "run_completed":
      return { ...state, completed: true };
    case "run_failed":
      return { ...state, completed: true, errorCode: readErrorCode(event.data) };
    default:
      return state;
  }
}

export function applyAgentReplay(
  prompt: string,
  replay: { run: AgentRun; steps: AgentStep[]; recommendations: Recommendation[] },
): AgentViewState {
  return {
    prompt,
    run: replay.run,
    steps: [...replay.steps].sort((left, right) => left.step_no - right.step_no),
    recommendations: [...replay.recommendations].sort((left, right) => left.rank_no - right.rank_no),
    completed: replay.run.status === "SUCCEEDED" || replay.run.status === "FAILED" || replay.run.status === "TIMEOUT",
    errorCode: replay.run.error_code,
  };
}

function upsertStep(steps: AgentStep[], step: AgentStep): AgentStep[] {
  const next = steps.filter((item) => item.step_no !== step.step_no);
  next.push(step);
  return next.sort((left, right) => left.step_no - right.step_no);
}

function upsertRecommendation(items: Recommendation[], recommendation: Recommendation): Recommendation[] {
  const next = items.filter((item) => item.sku_id !== recommendation.sku_id);
  next.push(recommendation);
  return next.sort((left, right) => left.rank_no - right.rank_no);
}

function readField<T>(value: unknown, key: string): T | undefined {
  if (value && typeof value === "object" && key in value) {
    return (value as Record<string, T>)[key];
  }
  return undefined;
}

function readErrorCode(value: unknown): StableErrorCode | undefined {
  if (value && typeof value === "object" && "code" in value) {
    return String((value as { code: unknown }).code);
  }
  return undefined;
}
