import { useEffect, useState } from "react";
import type { ProductSummary } from "../api/client";
import { api as defaultApi } from "../api/client";

type ProductListApi = {
  listProducts: (params?: { keyword?: string; page?: number; page_size?: number }) => Promise<{ products: ProductSummary[] }>;
};

type ProductListPageProps = {
  api?: ProductListApi;
};

export default function ProductListPage({ api = defaultApi }: ProductListPageProps) {
  const [keyword, setKeyword] = useState("");
  const [products, setProducts] = useState<ProductSummary[]>([]);
  const [status, setStatus] = useState<"loading" | "ready" | "empty" | "failed">("loading");

  useEffect(() => {
    let alive = true;
    setStatus("loading");
    api
      .listProducts({ keyword, page: 1, page_size: 20 })
      .then((result) => {
        if (!alive) {
          return;
        }
        setProducts(result.products);
        setStatus(result.products.length > 0 ? "ready" : "empty");
      })
      .catch(() => {
        if (alive) {
          setStatus("failed");
        }
      });
    return () => {
      alive = false;
    };
  }, [api, keyword]);

  return (
    <section className="pageStack">
      <div className="statusBanner compactBanner">
        <div>
          <p className="eyebrow">User shopping</p>
          <h1>导购精选，正在更新</h1>
          <p>浏览可售商品，或把自然语言购买需求交给 AI Guide。</p>
        </div>
        <a className="bannerAction" href={`#/guide?message=${encodeURIComponent(keyword)}`}>
          Ask AI
        </a>
      </div>

      <div className="toolbar">
        <label className="fieldLabel">
          Product search
          <input value={keyword} onChange={(event) => setKeyword(event.target.value)} placeholder="关键词、品牌或用途" />
        </label>
        <a className="secondaryButton" href={`#/guide?message=${encodeURIComponent(keyword)}`}>
          Ask AI
        </a>
      </div>

      {status === "loading" && <ProductSkeleton />}
      {status === "failed" && <div className="statePanel">商品暂时无法加载。</div>}
      {status === "empty" && <div className="statePanel">当前筛选条件下无商品。</div>}
      {status === "ready" && (
        <div className="productGrid">
          {products.map((product) => (
            <article className="productCard" key={product.product_id}>
              <div className="imageSlot" aria-hidden="true" />
              <div className="cardMeta">
                <p className="stockBadge">{product.stock_status || "UNKNOWN"}</p>
                <h2>{product.title}</h2>
                {product.subtitle && <p className="muted">{product.subtitle}</p>}
                <div className="cardFooter">
                  <strong>{product.min_sale_price ?? "待查询"}</strong>
                  <a href={`#/products/${product.product_id}`}>Detail</a>
                </div>
              </div>
            </article>
          ))}
        </div>
      )}
    </section>
  );
}

function ProductSkeleton() {
  return (
    <div className="productGrid" aria-label="Loading products">
      {[1, 2, 3, 4].map((value) => (
        <article className="productCard skeletonCard" key={value}>
          <div className="imageSlot" />
          <div className="cardMeta">
            <div className="skeletonLine short" />
            <div className="skeletonLine" />
            <div className="skeletonLine" />
          </div>
        </article>
      ))}
    </div>
  );
}
