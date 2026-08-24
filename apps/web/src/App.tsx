import { useEffect, useMemo, useState } from "react";
import "./styles.css";
import AuthPage from "./pages/AuthPage";
import CartPage from "./pages/CartPage";
import CheckoutPage from "./pages/CheckoutPage";
import GuidePage from "./pages/GuidePage";
import OrdersPage from "./pages/OrdersPage";
import OpsAgentRunsPage from "./pages/OpsAgentRunsPage";
import OpsDocumentsPage from "./pages/OpsDocumentsPage";
import OpsEventsPage from "./pages/OpsEventsPage";
import ProductDetailPage from "./pages/ProductDetailPage";
import ProductListPage from "./pages/ProductListPage";

export default function App() {
  const route = useHashRoute();

  return (
    <div className="appShell">
      <header className="topBar">
        <a className="brand" href="#/products" aria-label="智选购 首页">
          智选购
        </a>
        <nav aria-label="Primary" className="navLinks">
          <a href="#/products">Products</a>
          <a href="#/guide">AI Guide</a>
          <a href="#/cart">Cart</a>
          <a href="#/orders">Orders</a>
          <a href="#/ops/documents">Ops</a>
        </nav>
        <a className="accountLink" href="#/login">
          Login
        </a>
      </header>

      <main className="mainSurface">
        <RouteView route={route} />
      </main>
    </div>
  );
}

function RouteView({ route }: { route: URL }) {
  const path = route.pathname;
  if (path === "/login" || path === "/register") {
    return <AuthPage />;
  }
  if (path === "/guide") {
    return <GuidePage initialMessage={route.searchParams.get("message") ?? ""} />;
  }
  if (path === "/cart") {
    return <CartPage />;
  }
  if (path === "/checkout") {
    return <CheckoutPage />;
  }
  if (path === "/orders") {
    return <OrdersPage />;
  }
  if (path === "/ops/documents") {
    return <OpsDocumentsPage />;
  }
  if (path === "/ops/agent-runs") {
    return <OpsAgentRunsPage />;
  }
  if (path === "/ops/events") {
    return <OpsEventsPage />;
  }
  const productMatch = path.match(/^\/products\/(\d+)$/);
  if (productMatch) {
    return <ProductDetailPage productId={Number(productMatch[1])} />;
  }
  return <ProductListPage />;
}

function useHashRoute(): URL {
  const [hash, setHash] = useState(window.location.hash || "#/products");
  useEffect(() => {
    const listener = () => setHash(window.location.hash || "#/products");
    window.addEventListener("hashchange", listener);
    return () => window.removeEventListener("hashchange", listener);
  }, []);
  return useMemo(() => {
    const raw = hash.replace(/^#/, "") || "/products";
    return new URL(raw, "http://local");
  }, [hash]);
}
