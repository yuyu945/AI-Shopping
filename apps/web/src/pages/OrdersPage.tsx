import { useEffect, useState } from "react";
import type { Order, Review } from "../api/client";
import { api as defaultApi } from "../api/client";

type OrdersApi = {
  listOrders: () => Promise<{ orders: Order[] }>;
  getOrder: (orderNo: string) => Promise<{ order: Order }>;
  submitReview: (orderNo: string, skuID: number, input: { rating: number; content: string }) => Promise<{ review: Review }>;
};

type OrdersPageProps = {
  api?: OrdersApi;
};

export default function OrdersPage({ api = defaultApi }: OrdersPageProps) {
  const [orders, setOrders] = useState<Order[]>([]);
  const [reviewTarget, setReviewTarget] = useState<{ orderNo: string; skuID: number } | null>(null);
  const [rating, setRating] = useState(5);
  const [content, setContent] = useState("");
  const [reviewStatus, setReviewStatus] = useState("");

  useEffect(() => {
    let alive = true;
    api.listOrders().then((result) => {
      if (alive) {
        setOrders(result.orders);
      }
    });
    return () => {
      alive = false;
    };
  }, [api]);

  async function submitReview() {
    if (!reviewTarget) {
      return;
    }
    await api.submitReview(reviewTarget.orderNo, reviewTarget.skuID, { rating, content });
    setReviewStatus("评价已提交。");
  }

  return (
    <section className="pageStack">
      <div className="panelHeader">
        <div>
          <p className="eyebrow">Orders</p>
          <h1>订单</h1>
        </div>
      </div>
      {orders.length === 0 && <div className="statePanel">暂无订单。</div>}
      {orders.map((order) => (
        <article className="orderCard" key={order.order_no}>
          <div className="cardFooter">
            <strong>{order.order_no}</strong>
            <span className="stockBadge">{order.status}</span>
          </div>
          <p>
            Total {order.total_amount} / Paid {order.paid_amount}
          </p>
          {order.items.map((item) => (
            <div className="rowPanel" key={item.sku_id}>
              <span>{item.product_title}</span>
              <span>{item.sku_code}</span>
              {order.status === "PAID" && (
                <button className="secondaryButton" onClick={() => setReviewTarget({ orderNo: order.order_no, skuID: item.sku_id })} type="button">
                  Review item {item.sku_id}
                </button>
              )}
            </div>
          ))}
        </article>
      ))}

      {reviewTarget && (
        <aside className="sidePanel">
          <h2>提交评价</h2>
          <label className="fieldLabel">
            Rating
            <select value={rating} onChange={(event) => setRating(Number(event.target.value))}>
              {[5, 4, 3, 2, 1].map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </select>
          </label>
          <label className="fieldLabel">
            Review content
            <textarea value={content} onChange={(event) => setContent(event.target.value)} rows={4} />
          </label>
          <button className="primaryButton" onClick={submitReview} type="button">
            Submit review
          </button>
          {reviewStatus && <p className="successText">{reviewStatus}</p>}
        </aside>
      )}
    </section>
  );
}
