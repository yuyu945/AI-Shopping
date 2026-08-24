import { useEffect, useMemo, useState } from "react";
import type { Address, Order } from "../api/client";
import { api as defaultApi } from "../api/client";
import { submitWalletPayment } from "../domain/payment";

type CheckoutApi = {
  listAddresses: () => Promise<{ addresses?: Address[] }>;
  createOrder: (input: { request_id: string; address_id: number }) => Promise<{ order: Order }>;
  payWallet: (orderNo: string) => Promise<{ order: Order }>;
  getOrder: (orderNo: string) => Promise<{ order: Order } | Order>;
};

type CheckoutPageProps = {
  api?: CheckoutApi;
  initialOrder?: Order;
};

export default function CheckoutPage({ api = defaultApi, initialOrder }: CheckoutPageProps) {
  const [addresses, setAddresses] = useState<Address[]>([]);
  const [selectedAddressID, setSelectedAddressID] = useState<number>(0);
  const [order, setOrder] = useState<Order | undefined>(initialOrder);
  const [status, setStatus] = useState("");
  const requestID = useMemo(() => `web-${crypto.randomUUID ? crypto.randomUUID() : Date.now()}`, []);

  useEffect(() => {
    let alive = true;
    api.listAddresses().then((result) => {
      if (!alive) {
        return;
      }
      const list = result.addresses ?? [];
      setAddresses(list);
      setSelectedAddressID(list[0]?.address_id ?? 0);
    });
    return () => {
      alive = false;
    };
  }, [api]);

  async function createOrder() {
    if (!selectedAddressID) {
      setStatus("请选择地址。");
      return;
    }
    const result = await api.createOrder({ request_id: requestID, address_id: selectedAddressID });
    setOrder(result.order);
    setStatus("");
  }

  async function pay() {
    if (!order) {
      return;
    }
    setStatus("submitting");
    const result = await submitWalletPayment(order.order_no, {
      currentOrder: order,
      payWallet: async (orderNo) => (await api.payWallet(orderNo)).order,
      getOrder: async (orderNo) => unwrapOrder(await api.getOrder(orderNo)),
    });
    setOrder(result.order);
    setStatus(result.state === "blocked" ? result.errorCode : "");
  }

  return (
    <section className="checkoutLayout">
      <div className="detailMain">
        <p className="eyebrow">Checkout</p>
        <h1>订单确认</h1>
        <label className="fieldLabel">
          Address
          <select value={selectedAddressID} onChange={(event) => setSelectedAddressID(Number(event.target.value))}>
            <option value={0}>选择地址</option>
            {addresses.map((address) => (
              <option key={address.address_id} value={address.address_id}>
                {address.receiver_name} / {address.city}
              </option>
            ))}
          </select>
        </label>
        <button className="primaryButton" onClick={createOrder} type="button">
          Create order
        </button>
        {status && <p className="muted">{status}</p>}
      </div>

      <OrderSnapshot order={order} onPay={pay} />
    </section>
  );
}

function OrderSnapshot({ order, onPay }: { order?: Order; onPay: () => Promise<void> }) {
  if (!order) {
    return <div className="statePanel">订单快照会在创建后显示。</div>;
  }
  return (
    <aside className="sidePanel">
      <p className="eyebrow">Order snapshot</p>
      <h2>{order.order_no}</h2>
      <span className="stockBadge">{order.status}</span>
      <dl className="snapshotList">
        <div>
          <dt>Total</dt>
          <dd>{order.total_amount}</dd>
        </div>
        <div>
          <dt>Paid</dt>
          <dd>{order.paid_amount}</dd>
        </div>
      </dl>
      {order.items.map((item) => (
        <article className="rowPanel" key={item.sku_id}>
          <strong>{item.product_title}</strong>
          <span>{item.sku_code}</span>
          <span>{item.item_amount}</span>
        </article>
      ))}
      <button className="primaryButton" disabled={order.status === "PAID"} onClick={onPay} type="button">
        Pay wallet
      </button>
    </aside>
  );
}

function unwrapOrder(value: { order: Order } | Order): Order {
  return "order" in value ? value.order : value;
}
