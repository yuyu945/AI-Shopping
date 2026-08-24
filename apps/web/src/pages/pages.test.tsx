import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { Product } from "../api/client";
import App from "../App";
import AuthPage from "./AuthPage";
import CheckoutPage from "./CheckoutPage";
import GuidePage from "./GuidePage";
import OrdersPage from "./OrdersPage";
import OpsAgentRunsPage from "./OpsAgentRunsPage";
import OpsDocumentsPage from "./OpsDocumentsPage";
import OpsEventsPage from "./OpsEventsPage";
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

describe("Auth page", () => {
  it("stores access token after login", async () => {
    const api = {
      login: vi.fn().mockResolvedValue({ access_token: "jwt-token" }),
      register: vi.fn(),
      setToken: vi.fn(),
    };
    render(<AuthPage api={api} />);

    await userEvent.type(screen.getByLabelText("Email"), "user@example.com");
    await userEvent.type(screen.getByLabelText("Password"), "secret-password");
    await userEvent.click(screen.getByRole("button", { name: "Login" }));

    expect(api.setToken).toHaveBeenCalledWith("jwt-token");
    expect(await screen.findByText("已登录。")).toBeInTheDocument();
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

describe("Commerce pages", () => {
  it("creates an order with a stable request id from checkout", async () => {
    const api = fakeCheckoutApi();
    render(<CheckoutPage api={api} />);

    await userEvent.click(await screen.findByRole("button", { name: "Create order" }));

    expect(api.createOrder).toHaveBeenCalledWith(expect.objectContaining({ request_id: expect.any(String) }));
    expect(await screen.findByText("PENDING_PAYMENT")).toBeInTheDocument();
  });

  it("replays order after payment timeout", async () => {
    render(<CheckoutPage api={fakePaymentTimeoutApi()} initialOrder={pendingOrder()} />);

    await userEvent.click(screen.getByRole("button", { name: "Pay wallet" }));

    expect(await screen.findByText("PAID")).toBeInTheDocument();
  });

  it("submits a review for a paid order item", async () => {
    const api = fakeReviewApi();
    render(<OrdersPage api={api} />);

    await userEvent.click(await screen.findByRole("button", { name: "Review item 2001" }));
    await userEvent.selectOptions(screen.getByLabelText("Rating"), "5");
    await userEvent.type(screen.getByLabelText("Review content"), "体验稳定");
    await userEvent.click(screen.getByRole("button", { name: "Submit review" }));

    expect(api.submitReview).toHaveBeenCalledWith("order_1", 2001, { rating: 5, content: "体验稳定" });
  });
});

describe("Operations pages", () => {
  it("renders failed knowledge document and retries it", async () => {
    const api = {
      listKnowledgeDocuments: async () => ({ documents: [{ document_no: "doc_failed", product_id: 1001, doc_type: "FAQ", version: 2, status: "FAILED", error_code: "EMBEDDING_FAILED" }] }),
      getKnowledgeDocument: async () => ({ document: { document_no: "doc_failed", product_id: 1001, doc_type: "FAQ", version: 2, status: "FAILED" }, chunks: [] }),
      retryKnowledgeDocument: vi.fn().mockResolvedValue({ document: { document_no: "doc_failed", product_id: 1001, doc_type: "FAQ", version: 2, status: "PENDING" } }),
      uploadKnowledgeDocument: vi.fn().mockResolvedValue({ document: { document_no: "doc_uploaded", product_id: 1001, doc_type: "FAQ", version: 3, status: "PENDING" } }),
    };
    render(<OpsDocumentsPage api={api} />);

    await userEvent.upload(screen.getByLabelText("Upload file"), new File(["faq"], "faq.txt", { type: "text/plain" }));
    await userEvent.click(screen.getByRole("button", { name: "Upload" }));
    expect(api.uploadKnowledgeDocument).toHaveBeenCalledWith(expect.objectContaining({ product_id: 1001, doc_type: "FAQ", file_name: "faq.txt" }));
    expect(await screen.findByText("Upload submitted")).toBeInTheDocument();

    await userEvent.click(await screen.findByRole("button", { name: "doc_failed" }));
    await userEvent.click(screen.getByRole("button", { name: "Retry" }));

    expect(api.retryKnowledgeDocument).toHaveBeenCalledWith("doc_failed");
    expect(await screen.findByText("Retry submitted")).toBeInTheDocument();
    expect(screen.getAllByText("PENDING").length).toBeGreaterThan(0);
  });

  it("renders agent ops timeline with trace and redacted JSON", async () => {
    const api = {
      listAgentRunsOps: async () => ({ runs: [{ run_id: "run_1", status: "SUCCEEDED", trace_id: "trace_1", step_count: 1 }] }),
      getAgentRunOps: async () => ({
        run: { run_id: "run_1", status: "SUCCEEDED", trace_id: "trace_1", model_name: "qwen-plus" },
        steps: [{ step_no: 1, tool_name: "get_user_profile", status: "SUCCEEDED", input_json: { phone: "[REDACTED]" }, output_json: { user_id: 7 }, latency_ms: 12 }],
        recommendations: [],
      }),
    };
    render(<OpsAgentRunsPage api={api} />);

    await userEvent.click(await screen.findByRole("button", { name: "run_1" }));

    expect((await screen.findAllByText("trace_1")).length).toBeGreaterThanOrEqual(2);
    expect(screen.getByText(/REDACTED/)).toBeInTheDocument();
    expect(screen.getByText(/12ms/)).toBeInTheDocument();
  });

  it("renders event overview behavior and dead letters", async () => {
    render(
      <OpsEventsPage
        api={{
          getEventOverview: async () => ({
            behavior_events: [{ event_id: "event_1", user_id: 7, event_type: "product.viewed", trace_id: "trace_1", resource_type: "product", resource_id: "1001" }],
            review_events: [],
            product_stats: [],
            dead_letters: [{ topic: "behavior.events", event_key: "7", reason: "invalid_behavior_event" }],
          }),
        }}
      />,
    );

    expect(await screen.findByText("product.viewed")).toBeInTheDocument();
    expect(screen.getByText("invalid_behavior_event")).toBeInTheDocument();
  });
});

function fakeCheckoutApi() {
  const createOrder = vi.fn().mockResolvedValue({ order: pendingOrder() });
  return {
    listAddresses: async () => ({ addresses: [{ address_id: 7, receiver_name: "张三", receiver_phone: "13800000000", province: "上海", city: "上海", district: "浦东", detail: "测试路 1 号" }] }),
    createOrder,
    payWallet: vi.fn(),
    getOrder: vi.fn(),
  };
}

function fakePaymentTimeoutApi() {
  return {
    listAddresses: async () => ({ addresses: [] }),
    createOrder: vi.fn(),
    payWallet: vi.fn().mockRejectedValue({ code: "DEPENDENCY_TIMEOUT" }),
    getOrder: vi.fn().mockResolvedValue(paidOrder()),
  };
}

function fakeReviewApi() {
  return {
    listOrders: async () => ({ orders: [paidOrder()] }),
    getOrder: async () => ({ order: paidOrder() }),
    submitReview: vi.fn().mockResolvedValue({ review: { review_no: "review_1", order_no: "order_1", product_id: 1001, sku_id: 2001, rating: 5, content: "体验稳定", status: "PUBLISHED" } }),
  };
}

function paidOrder() {
  return { ...pendingOrder(), status: "PAID", paid_amount: "4999.00" };
}

function pendingOrder() {
  return {
    order_no: "order_1",
    request_id: "req_1",
    status: "PENDING_PAYMENT",
    total_amount: "4999.00",
    paid_amount: "0.00",
    shipping_address: { receiver_name: "张三", receiver_phone: "13800000000", province: "上海", city: "上海", district: "浦东", detail: "测试路 1 号" },
    items: [
      {
        product_id: 1001,
        sku_id: 2001,
        product_title: "Laptop",
        sku_code: "LAPTOP-16G",
        sku_spec_json: { memory: "16GB" },
        unit_price: "4999.00",
        discount_amount: "0.00",
        quantity: 1,
        item_amount: "4999.00",
      },
    ],
  };
}

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
