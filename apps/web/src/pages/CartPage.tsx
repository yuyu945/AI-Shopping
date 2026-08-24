import { useEffect, useState } from "react";
import type { Cart } from "../api/client";
import { api as defaultApi } from "../api/client";

type CartApi = {
  getCart: () => Promise<{ cart: Cart }>;
  updateCartItem: (cartItemID: number, input: { quantity: number; selected: boolean }) => Promise<void>;
  deleteCartItem: (cartItemID: number) => Promise<void>;
};

type CartPageProps = {
  api?: CartApi;
};

export default function CartPage({ api = defaultApi }: CartPageProps) {
  const [cart, setCart] = useState<Cart>({ items: [] });
  const [status, setStatus] = useState("loading");

  useEffect(() => {
    let alive = true;
    api
      .getCart()
      .then((result) => {
        if (alive) {
          setCart(result.cart);
          setStatus("ready");
        }
      })
      .catch(() => {
        if (alive) {
          setStatus("failed");
        }
      });
    return () => {
      alive = false;
    };
  }, [api]);

  async function setSelected(cartItemID: number, quantity: number, selected: boolean) {
    await api.updateCartItem(cartItemID, { quantity, selected });
    setCart((current) => ({
      items: current.items.map((item) => (item.cart_item_id === cartItemID ? { ...item, selected } : item)),
    }));
  }

  async function remove(cartItemID: number) {
    await api.deleteCartItem(cartItemID);
    setCart((current) => ({ items: current.items.filter((item) => item.cart_item_id !== cartItemID) }));
  }

  return (
    <section className="pageStack">
      <div className="panelHeader">
        <div>
          <p className="eyebrow">Cart</p>
          <h1>购物车</h1>
        </div>
        <a className="primaryButton" href="#/checkout">
          Checkout
        </a>
      </div>
      {status === "loading" && <div className="statePanel">购物车加载中。</div>}
      {status === "failed" && <div className="statePanel">购物车暂时无法加载。</div>}
      {status === "ready" && cart.items.length === 0 && <div className="statePanel">购物车为空。</div>}
      {cart.items.map((item) => (
        <article className="rowPanel" key={item.cart_item_id}>
          <label>
            <input
              checked={item.selected}
              onChange={(event) => setSelected(item.cart_item_id, item.quantity, event.target.checked)}
              type="checkbox"
            />
            SKU {item.sku_id}
          </label>
          <span>Quantity {item.quantity}</span>
          <button className="secondaryButton" onClick={() => remove(item.cart_item_id)} type="button">
            Delete
          </button>
        </article>
      ))}
    </section>
  );
}
