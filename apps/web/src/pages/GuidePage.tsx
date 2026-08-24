import { useState } from "react";
import type { AgentEvent, AgentRun, AgentTimeline, CartItem } from "../api/client";
import { api as defaultApi, subscribeAgentRunEvents as defaultSubscribeAgentRunEvents } from "../api/client";
import { applyAgentEvent, applyAgentReplay, emptyAgentState, type AgentViewState } from "../domain/agent";

type GuideApi = {
  startAgentRun: (input: { message: string; session_no?: string }) => Promise<{ run: AgentRun }>;
  subscribeAgentRunEvents: (runID: string, onEvent: (event: AgentEvent) => void) => Promise<void>;
  getAgentRun: (runID: string) => Promise<AgentTimeline>;
  addCartItem: (input: { sku_id: number; quantity: number; selected: boolean }) => Promise<{ item: CartItem }>;
};

type GuidePageProps = {
  api?: GuideApi;
  initialMessage?: string;
};

const defaultGuideApi: GuideApi = {
  startAgentRun: defaultApi.startAgentRun,
  subscribeAgentRunEvents: defaultSubscribeAgentRunEvents,
  getAgentRun: defaultApi.getAgentRun,
  addCartItem: defaultApi.addCartItem,
};

export default function GuidePage({ api = defaultGuideApi, initialMessage = "" }: GuidePageProps) {
  const [message, setMessage] = useState(initialMessage);
  const [state, setState] = useState<AgentViewState>(emptyAgentState(initialMessage));
  const [submitting, setSubmitting] = useState(false);
  const [cartMessage, setCartMessage] = useState("");

  async function send() {
    const prompt = message.trim();
    if (!prompt || submitting) {
      return;
    }
    setSubmitting(true);
    let nextState = emptyAgentState(prompt);
    setState(nextState);
    try {
      const started = await api.startAgentRun({ message: prompt });
      nextState = { ...nextState, run: started.run };
      setState(nextState);
      await api.subscribeAgentRunEvents(started.run.run_id, (event) => {
        setState((current) => applyAgentEvent(current, event));
      });
    } catch {
      if (nextState.run?.run_id) {
        const replay = await api.getAgentRun(nextState.run.run_id);
        setState(applyAgentReplay(prompt, replay));
      } else {
        setState({ ...nextState, completed: true, errorCode: "AGENT_RUN_FAILED" });
      }
    } finally {
      setSubmitting(false);
    }
  }

  async function addRecommendationToCart(skuID: number) {
    await api.addCartItem({ sku_id: skuID, quantity: 1, selected: true });
    setCartMessage("已加入购物车。");
  }

  return (
    <section className="guideLayout">
      <div className="conversationPanel">
        <p className="eyebrow">AI Guide</p>
        <h1>自然语言导购</h1>
        <div className="messageBubble userBubble">{state.prompt || "描述预算、用途和偏好。"}</div>
        {state.errorCode && <div className="errorPanel">导购服务暂时无法完成，请调整条件后重试。</div>}

        <div className="timeline" aria-label="Agent timeline">
          {state.steps.length === 0 && <div className="statePanel">等待创建 Agent Run。</div>}
          {state.steps.map((step) => (
            <article className="timelineItem" key={step.step_no}>
              <span>{String(step.step_no).padStart(2, "0")}</span>
              <strong>{step.tool_name || step.step_type || "step"}</strong>
              <em>{step.status}</em>
            </article>
          ))}
        </div>

        <label className="fieldLabel guideInput">
          Purchase request
          <textarea value={message} onChange={(event) => setMessage(event.target.value)} rows={4} />
        </label>
        <button className="primaryButton" disabled={submitting || !message.trim()} onClick={send} type="button">
          Send
        </button>
      </div>

      <aside className="recommendationPanel">
        <div className="panelHeader">
          <div>
            <p className="eyebrow">Verified recommendations</p>
            <h2>{state.run?.status || "READY"}</h2>
          </div>
          {state.completed && <span className="stockBadge">DONE</span>}
        </div>
        <div className="recommendationList">
          {state.recommendations.length === 0 && <div className="statePanel">推荐结果会在后端校验后显示。</div>}
          {state.recommendations.map((item) => (
            <article className="recommendationCard" key={item.sku_id}>
              <div className="cardFooter">
                <strong>{item.product_title || `SKU ${item.sku_id}`}</strong>
                <span className="stockBadge">{item.validation_status || "PENDING"}</span>
              </div>
              <p>{item.reason || "AI 推荐理由待回放。"}</p>
              <dl className="snapshotList">
                <div>
                  <dt>SKU</dt>
                  <dd>{item.sku_id}</dd>
                </div>
                <div>
                  <dt>Price</dt>
                  <dd>{item.price || "后端快照"}</dd>
                </div>
                <div>
                  <dt>Saleable</dt>
                  <dd>{String(item.saleable ?? true)}</dd>
                </div>
              </dl>
              <div className="actionRow">
                {item.product_id && <a className="secondaryButton" href={`#/products/${item.product_id}`}>Detail</a>}
                <button className="primaryButton" onClick={() => addRecommendationToCart(item.sku_id)} type="button">
                  Add to cart
                </button>
              </div>
            </article>
          ))}
        </div>
        {cartMessage && <p className="successText">{cartMessage}</p>}
      </aside>
    </section>
  );
}
