import { useEffect, useMemo, useState } from "react";
import type { KnowledgeSnippet, Product } from "../api/client";
import { api as defaultApi } from "../api/client";

type ProductDetailApi = {
  getProduct: (productID: number, skuID?: number) => Promise<{ product: Product }>;
  addCartItem: (input: { sku_id: number; quantity: number; selected: boolean }) => Promise<unknown>;
  askProductQuestion: (
    productID: number,
    input: { question: string; doc_types?: string[]; top_k?: number },
  ) => Promise<{ snippets: KnowledgeSnippet[]; fallback_reason: string }>;
};

type ProductDetailPageProps = {
  productId: number;
  api?: ProductDetailApi;
};

export default function ProductDetailPage({ productId, api = defaultApi }: ProductDetailPageProps) {
  const [product, setProduct] = useState<Product | null>(null);
  const [selectedSkuID, setSelectedSkuID] = useState<number | undefined>();
  const [loading, setLoading] = useState(true);
  const [cartStatus, setCartStatus] = useState("");
  const [question, setQuestion] = useState("");
  const [answer, setAnswer] = useState<{ snippets: KnowledgeSnippet[]; fallback_reason: string } | null>(null);
  const [answering, setAnswering] = useState(false);

  useEffect(() => {
    let alive = true;
    setLoading(true);
    api
      .getProduct(productId, selectedSkuID)
      .then((result) => {
        if (!alive) {
          return;
        }
        setProduct(result.product);
        if (!selectedSkuID) {
          setSelectedSkuID(result.product.skus[0]?.sku_id);
        }
      })
      .finally(() => {
        if (alive) {
          setLoading(false);
        }
      });
    return () => {
      alive = false;
    };
  }, [api, productId, selectedSkuID]);

  const selectedSku = useMemo(
    () => product?.skus.find((sku) => sku.sku_id === selectedSkuID) ?? product?.skus[0],
    [product, selectedSkuID],
  );

  async function addSelectedSku() {
    if (!selectedSku) {
      return;
    }
    setCartStatus("adding");
    await api.addCartItem({ sku_id: selectedSku.sku_id, quantity: 1, selected: true });
    setCartStatus("added");
  }

  async function askQuestion() {
    const trimmed = question.trim();
    if (!trimmed) {
      return;
    }
    setAnswering(true);
    const result = await api.askProductQuestion(productId, { question: trimmed, top_k: 3 });
    setAnswer(result);
    setAnswering(false);
  }

  if (!product) {
    return <div className="statePanel">商品资料加载中。</div>;
  }

  const saleable = selectedSku?.stock_status === "IN_STOCK" && (selectedSku.stock_qty ?? 0) > 0;

  return (
    <section className="detailLayout">
      <div className="detailMain">
        <p className="eyebrow">Product detail</p>
        <h1>{product.title}</h1>
        {product.subtitle && <p className="muted">{product.subtitle}</p>}

        <div className="skuPanel" aria-label="SKU options">
          {product.skus.map((sku) => (
            <button
              className={sku.sku_id === selectedSkuID ? "skuButton selected" : "skuButton"}
              key={sku.sku_id}
              onClick={() => setSelectedSkuID(sku.sku_id)}
              type="button"
            >
              {Object.values(sku.specs).join(" / ") || sku.sku_code}
            </button>
          ))}
        </div>

        <div className="factGrid">
          <div>
            <span>Price</span>
            <strong>{selectedSku?.sale_price ?? "待查询"}</strong>
          </div>
          <div>
            <span>Stock</span>
            <strong>{selectedSku?.stock_status ?? "UNKNOWN"}</strong>
          </div>
          <div>
            <span>SKU</span>
            <strong>{selectedSku?.sku_code ?? "-"}</strong>
          </div>
        </div>

        <button className="primaryButton" disabled={loading || !saleable || cartStatus === "adding"} onClick={addSelectedSku} type="button">
          Add to cart
        </button>
        {cartStatus === "added" && <p className="successText">已加入购物车。</p>}

        <section className="textPanel">
          <h2>详情</h2>
          <p>{product.detail_markdown || "暂无详情资料。"}</p>
        </section>
      </div>

      <aside className="sidePanel">
        <h2>商品问答</h2>
        <label className="fieldLabel">
          Product question
          <textarea value={question} onChange={(event) => setQuestion(event.target.value)} rows={4} />
        </label>
        <button className="secondaryButton" disabled={answering} onClick={askQuestion} type="button">
          Ask
        </button>
        {answer && <KnowledgeAnswer answer={answer} />}
      </aside>
    </section>
  );
}

function KnowledgeAnswer({ answer }: { answer: { snippets: KnowledgeSnippet[]; fallback_reason: string } }) {
  if (answer.fallback_reason || answer.snippets.length === 0) {
    return <div className="statePanel">资料中没有足够信息回答该问题。</div>;
  }
  return (
    <div className="sourceList">
      {answer.snippets.map((snippet) => (
        <article className="sourceItem" key={snippet.chunk_id}>
          <strong>
            {snippet.doc_type} v{snippet.version}
          </strong>
          <span>
            {snippet.section} / page {snippet.source_page}
          </span>
          <p>{snippet.content}</p>
        </article>
      ))}
    </div>
  );
}
