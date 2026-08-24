import { useEffect, useState } from "react";
import { api, type EventOverview } from "../api/client";

type OpsEventsAPI = Pick<typeof api, "getEventOverview">;

export default function OpsEventsPage({ api: client = api }: { api?: OpsEventsAPI }) {
  const [overview, setOverview] = useState<EventOverview | null>(null);

  useEffect(() => {
    void client.getEventOverview().then(setOverview);
  }, []);

  return (
    <section className="pageStack">
      <section className="statusBanner compactBanner">
        <div>
          <p className="eyebrow">Operations</p>
          <h1>基础事件概览</h1>
          <p>只读查看 review.events、behavior.events 和 dead-letter 记录。</p>
        </div>
      </section>
      {!overview ? (
        <div className="statePanel">Loading events...</div>
      ) : (
        <>
          <div className="factGrid">
            <div><strong>{overview.behavior_events.length}</strong><span>Behavior events</span></div>
            <div><strong>{overview.review_events.length}</strong><span>Review events</span></div>
            <div><strong>{overview.dead_letters.length}</strong><span>Dead letters</span></div>
          </div>
          <div className="opsEventGrid">
            <section className="sidePanel">
              <h2>Behavior</h2>
              {overview.behavior_events.map((event) => (
                <div className="rowPanel" key={event.event_id}>
                  <strong>{event.event_type}</strong>
                  <span>{event.resource_type}:{event.resource_id}</span>
                  <em>{event.trace_id}</em>
                </div>
              ))}
            </section>
            <section className="sidePanel">
              <h2>Reviews</h2>
              {overview.product_stats.map((stat) => (
                <div className="rowPanel" key={stat.product_id}>
                  <strong>Product {stat.product_id}</strong>
                  <span>{stat.review_count} reviews</span>
                  <em>{stat.rating_avg}</em>
                </div>
              ))}
              {overview.review_events.map((event) => (
                <div className="rowPanel" key={event.event_id}>
                  <strong>{event.review_no}</strong>
                  <span>SKU {event.sku_id}</span>
                  <em>{event.rating}</em>
                </div>
              ))}
            </section>
            <section className="sidePanel">
              <h2>Dead Letters</h2>
              {overview.dead_letters.map((event) => (
                <div className="rowPanel" key={`${event.topic}-${event.event_key}-${event.reason}`}>
                  <strong>{event.topic}</strong>
                  <span>{event.event_key}</span>
                  <em>{event.reason}</em>
                </div>
              ))}
            </section>
          </div>
        </>
      )}
    </section>
  );
}
