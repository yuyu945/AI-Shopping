import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { Product } from "../api/client";
import App from "../App";
import GuidePage from "./GuidePage";
import ProductDetailPage from "./ProductDetailPage";
import ProductListPage from "./ProductListPage";

describe("App shell", () => {
  it("renders the prototype-aligned user shopping navigation", () => {
    render(<App />);

    expect(screen.getByRole("navigation", { name: "Primary" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Products" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "AI Guide" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Cart" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Orders" })).toBeInTheDocument();
    expect(screen.getByText("导购精选，正在更新")).toBeInTheDocument();
  });
});

describe("Product pages", () => {
  it("renders product cards from Gateway DTOs", async () => {
    render(
      <ProductListPage
        api={{
          listProducts: async () => ({
            products: [{ product_id: 1, title: "Laptop", min_sale_price: "4999.00", stock_status: "IN_STOCK" }],
          }),
        }}
      />,
    );

    expect(await screen.findByText("Laptop")).toBeInTheDocument();
    expect(screen.getByText("4999.00")).toBeInTheDocument();
  });

  it("disables add-to-cart while SKU switching reloads", async () => {
    const user = userEvent.setup();
    render(<ProductDetailPage api={fakeProductDetailApi()} productId={1} />);

    await screen.findByText("16GB");
    await user.click(screen.getByRole("button", { name: /32GB/ }));

    expect(screen.getByRole("button", { name: "Add to cart" })).toBeDisabled();
  });

  it("shows knowledge sources and no-evidence fallback", async () => {
    const user = userEvent.setup();
    render(
      <ProductDetailPage
        api={fakeProductDetailApi({
          snippets: [],
          fallback_reason: "NO_READY_DOCUMENT",
        })}
        productId={1}
      />,
    );

    await user.type(await screen.findByLabelText("Product question"), "保修多久");
    await user.click(screen.getByRole("button", { name: "Ask" }));

    expect(await screen.findByText("资料中没有足够信息回答该问题。")).toBeInTheDocument();
  });
});

describe("Guide page", () => {
  it("starts an agent run and renders verified recommendations", async () => {
    const user = userEvent.setup();
    render(<GuidePage api={fakeGuideApiWithEvents()} initialMessage="预算 5000 买电脑" />);

    await user.click(screen.getByRole("button", { name: "Send" }));

    expect(await screen.findByText("search_products")).toBeInTheDocument();
    expect(await screen.findByText("VERIFIED")).toBeInTheDocument();
  });

  it("keeps prompt visible after controlled run failure", async () => {
    render(<GuidePage api={fakeFailedGuideApi()} initialMessage="买手机" />);

    await userEvent.click(screen.getByRole("button", { name: "Send" }));

    expect(await screen.findAllByText("买手机")).toHaveLength(2);
    expect(screen.getByText("导购服务暂时无法完成，请调整条件后重试。")).toBeInTheDocument();
  });
});

function fakeGuideApiWithEvents() {
  return {
    startAgentRun: async () => ({ run: { run_id: "run_1", status: "RUNNING", stream_url: "/events" } }),
    subscribeAgentRunEvents: async (_runID: string, onEvent: (event: { type: string; data: unknown }) => void) => {
      onEvent({ type: "step_snapshot", data: { step: { step_no: 1, tool_name: "search_products", status: "SUCCEEDED" } } });
      onEvent({
        type: "recommendation_snapshot",
        data: {
          recommendation: {
            sku_id: 2001,
            rank_no: 1,
            product_id: 1001,
            product_title: "Laptop",
            price: "4999.00",
            validation_status: "VERIFIED",
            reason: "预算内，适合编程。",
          },
        },
      });
      onEvent({ type: "run_completed", data: { run_id: "run_1", status: "SUCCEEDED" } });
    },
    getAgentRun: async () => ({
      run: { run_id: "run_1", status: "SUCCEEDED" },
      steps: [],
      recommendations: [],
    }),
    addCartItem: async () => ({ item: { cart_item_id: 1, sku_id: 2001, quantity: 1, selected: true } }),
  };
}

function fakeFailedGuideApi() {
  return {
    ...fakeGuideApiWithEvents(),
    startAgentRun: async () => ({ run: { run_id: "run_2", status: "RUNNING", stream_url: "/events" } }),
    subscribeAgentRunEvents: async (_runID: string, onEvent: (event: { type: string; data: unknown }) => void) => {
      onEvent({ type: "run_failed", data: { code: "AGENT_RUN_FAILED" } });
    },
  };
}

function fakeProductDetailApi(
  answer: {
    snippets: Array<{ chunk_id: string; document_no: string; doc_type: string; version: number; section: string; source_page: number; content: string; score: number; product_id: number }>;
    fallback_reason: string;
  } = {
    snippets: [
      {
        chunk_id: "chunk_1",
        document_no: "doc_1",
        product_id: 1,
        doc_type: "FAQ",
        version: 1,
        section: "保修",
        source_page: 2,
        content: "整机保修一年。",
        score: 0.82,
      },
    ],
    fallback_reason: "",
  },
) {
  return {
    getProduct: async (_productId: number, skuId?: number) => {
      if (skuId === 102) {
        await new Promise(() => undefined);
      }
      return { product: productFixture };
    },
    addCartItem: async () => ({
      item: { cart_item_id: 1, sku_id: 101, quantity: 1, selected: true },
    }),
    askProductQuestion: async () => answer,
  };
}

const productFixture: Product = {
  product_id: 1,
  category_id: 10,
  title: "Laptop",
  subtitle: "适合编程与轻度游戏",
  detail_markdown: "高性能轻薄本",
  images: [],
  promotions: [{ promotion_id: 1, rule_type: "FULL_REDUCTION", threshold_amount: "5000.00", discount_amount: "300.00" }],
  skus: [
    { sku_id: 101, sku_code: "LAPTOP-16G", specs: { memory: "16GB" }, sale_price: "4999.00", stock_qty: 8, stock_status: "IN_STOCK" },
    { sku_id: 102, sku_code: "LAPTOP-32G", specs: { memory: "32GB" }, sale_price: "5999.00", stock_qty: 3, stock_status: "IN_STOCK" },
  ],
};
