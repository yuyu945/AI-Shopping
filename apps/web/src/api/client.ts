const tokenKey = "ai-shopping-token";
const defaultBaseURL = "http://localhost:8080";

type Fetcher = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

export type StableErrorCode =
  | "INVALID_ARGUMENT"
  | "UNAUTHENTICATED"
  | "NOT_FOUND"
  | "OUT_OF_STOCK"
  | "INSUFFICIENT_BALANCE"
  | "PAYMENT_IN_PROGRESS"
  | "AGENT_RUN_FAILED"
  | "DEPENDENCY_TIMEOUT"
  | "NETWORK_ERROR"
  | "REQUEST_TIMEOUT"
  | "INTERNAL"
  | string;

export class ApiError extends Error {
  readonly code: StableErrorCode;
  readonly status: number;

  constructor(code: StableErrorCode, message: string, status = 0) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
  }
}

export type ProductSummary = {
  product_id: number;
  category_id?: number;
  title: string;
  subtitle?: string;
  min_sale_price?: string;
  stock_qty?: number;
  stock_status?: string;
};

export type Sku = {
  sku_id: number;
  sku_code: string;
  specs: Record<string, string>;
  sale_price: string;
  stock_qty: number;
  stock_status: string;
};

export type Product = {
  product_id: number;
  category_id?: number;
  title: string;
  subtitle?: string;
  detail_markdown?: string;
  skus: Sku[];
  images: Array<{ image_id: number; object_key: string; sort_no: number }>;
  promotions: Array<{ promotion_id: number; rule_type: string; threshold_amount?: string; discount_amount?: string }>;
};

export type CartItem = {
  cart_item_id: number;
  sku_id: number;
  quantity: number;
  selected: boolean;
};

export type Cart = {
  items: CartItem[];
};

export type Address = {
  address_id?: number;
  receiver_name: string;
  receiver_phone: string;
  province: string;
  city: string;
  district: string;
  detail: string;
  is_default?: boolean;
};

export type OrderItem = {
  product_id: number;
  sku_id: number;
  product_title: string;
  sku_code: string;
  sku_spec_json: Record<string, string>;
  unit_price: string;
  discount_amount: string;
  quantity: number;
  item_amount: string;
};

export type Order = {
  order_no: string;
  request_id: string;
  status: string;
  total_amount: string;
  paid_amount: string;
  shipping_address?: Address;
  items: OrderItem[];
};

export type Review = {
  review_no: string;
  order_no: string;
  product_id: number;
  sku_id: number;
  rating: number;
  content: string;
  status: string;
};

export type AgentRun = {
  run_id: string;
  session_no?: string;
  status: string;
  final_text?: string;
  error_code?: string;
  step_count?: number;
  stream_url?: string;
};

export type AgentStep = {
  step_no: number;
  step_type?: string;
  tool_name?: string;
  status: string;
  error_code?: string;
  latency_ms?: number;
};

export type Recommendation = {
  rank_no: number;
  sku_id: number;
  product_id?: number;
  product_title?: string;
  sku_code?: string;
  sku_spec_json?: Record<string, string>;
  price?: string;
  saleable?: boolean;
  discount_json?: unknown[];
  reason?: string;
  validation_status?: string;
};

export type KnowledgeSnippet = {
  chunk_id: string;
  document_no: string;
  product_id: number;
  doc_type: string;
  version: number;
  section: string;
  source_page: number;
  content: string;
  score: number;
};

export type AgentTimeline = {
  run: AgentRun;
  steps: AgentStep[];
  recommendations: Recommendation[];
};

export function getToken(): string {
  return window.localStorage.getItem(tokenKey) ?? "";
}

export function setToken(token: string): void {
  window.localStorage.setItem(tokenKey, token);
}

export function clearToken(): void {
  window.localStorage.removeItem(tokenKey);
}

export async function apiFetch<T>(path: string, options: RequestInit = {}, fetcher: Fetcher = fetch): Promise<T> {
  const init = withHeaders(options);
  try {
    const response = await fetcher(toURL(path), init);
    const data = await readJSON(response);
    if (!response.ok) {
      const errorBody = asErrorBody(data);
      throw new ApiError(errorBody.code, errorBody.message, response.status);
    }
    return data as T;
  } catch (error) {
    if (error instanceof ApiError) {
      throw error;
    }
    if (error instanceof DOMException && error.name === "AbortError") {
      throw new ApiError("REQUEST_TIMEOUT", "request timeout");
    }
    throw new ApiError("NETWORK_ERROR", "network error");
  }
}

export const api = {
  login: (input: { email: string; password: string }) =>
    apiFetch<{ access_token?: string; token?: string }>("/api/v1/auth/login", jsonRequest("POST", input)),
  register: (input: { email: string; password: string }) =>
    apiFetch<{ access_token?: string; token?: string }>("/api/v1/auth/register", jsonRequest("POST", input)),
  listProducts: (params: { keyword?: string; category_id?: number; page?: number; page_size?: number } = {}) =>
    apiFetch<{ products: ProductSummary[] }>(withQuery("/api/v1/products", params)),
  getProduct: (productID: number, skuID?: number) =>
    apiFetch<{ product: Product }>(withQuery(`/api/v1/products/${productID}`, skuID ? { sku_id: skuID } : {})),
  getCart: () => apiFetch<{ cart: Cart }>("/api/v1/cart"),
  addCartItem: (input: { sku_id: number; quantity: number; selected: boolean }) =>
    apiFetch<{ item: CartItem }>("/api/v1/cart/items", jsonRequest("POST", input)),
  updateCartItem: (cartItemID: number, input: { quantity: number; selected: boolean }) =>
    apiFetch<void>(`/api/v1/cart/items/${cartItemID}`, jsonRequest("PUT", input)),
  deleteCartItem: (cartItemID: number) => apiFetch<void>(`/api/v1/cart/items/${cartItemID}`, { method: "DELETE" }),
  listAddresses: () => apiFetch<{ addresses?: Address[] }>("/api/v1/users/me/addresses"),
  createOrder: (input: { request_id: string; address_id: number }) =>
    apiFetch<{ order: Order }>("/api/v1/orders", jsonRequest("POST", input)),
  listOrders: () => apiFetch<{ orders: Order[] }>("/api/v1/orders"),
  getOrder: (orderNo: string) => apiFetch<{ order: Order }>(`/api/v1/orders/${orderNo}`),
  payWallet: (orderNo: string) => apiFetch<{ order: Order }>(`/api/v1/orders/${orderNo}/payments/wallet`, { method: "POST" }),
  submitReview: (orderNo: string, skuID: number, input: { rating: number; content: string }) =>
    apiFetch<{ review: Review }>(`/api/v1/orders/${orderNo}/items/${skuID}/reviews`, jsonRequest("POST", input)),
  startAgentRun: (input: { session_no?: string; message: string }) =>
    apiFetch<{ run: AgentRun }>("/api/v1/agent/runs", jsonRequest("POST", input)),
  getAgentRun: (runID: string) => apiFetch<AgentTimeline>(`/api/v1/agent/runs/${runID}`),
  askProductQuestion: (productID: number, input: { question: string; doc_types?: string[]; top_k?: number }) =>
    apiFetch<{ snippets: KnowledgeSnippet[]; fallback_reason: string }>(
      `/api/v1/products/${productID}/knowledge/questions`,
      jsonRequest("POST", input),
    ),
};

export type AgentEvent = {
  type: string;
  data: unknown;
};

export async function subscribeAgentRunEvents(
  runID: string,
  onEvent: (event: AgentEvent) => void,
  fetcher: Fetcher = fetch,
): Promise<void> {
  const response = await fetcher(toURL(`/api/v1/agent/runs/${runID}/events`), withHeaders({ method: "GET" }));
  if (!response.ok || !response.body) {
    throw new ApiError("DEPENDENCY_TIMEOUT", "agent event stream unavailable", response.status);
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  for (;;) {
    const read = await reader.read();
    if (read.done) {
      break;
    }
    buffer += decoder.decode(read.value, { stream: true });
    const chunks = buffer.split("\n\n");
    buffer = chunks.pop() ?? "";
    for (const chunk of chunks) {
      const event = parseSSEChunk(chunk);
      if (event) {
        onEvent(event);
      }
    }
  }
}

function withHeaders(options: RequestInit): RequestInit {
  const headers = headersToRecord(options.headers);
  const token = getToken();
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  if (options.body && !headers["Content-Type"]) {
    headers["Content-Type"] = "application/json";
  }
  return { ...options, headers };
}

function headersToRecord(headers: HeadersInit | undefined): Record<string, string> {
  if (!headers) {
    return {};
  }
  if (headers instanceof Headers) {
    return Object.fromEntries(headers.entries());
  }
  if (Array.isArray(headers)) {
    return Object.fromEntries(headers);
  }
  return { ...headers };
}

function jsonRequest(method: "POST" | "PUT", body: unknown): RequestInit {
  return { method, body: JSON.stringify(body) };
}

function toURL(path: string): string {
  if (/^https?:\/\//.test(path)) {
    return path;
  }
  const base = (import.meta.env.VITE_API_BASE_URL as string | undefined) || defaultBaseURL;
  return `${base}${path}`;
}

function withQuery(path: string, params: Record<string, string | number | undefined>): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== "") {
      query.set(key, String(value));
    }
  }
  const encoded = query.toString();
  return encoded ? `${path}?${encoded}` : path;
}

async function readJSON(response: Response): Promise<unknown> {
  if (response.status === 204) {
    return undefined;
  }
  const text = await response.text();
  if (!text) {
    return undefined;
  }
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return undefined;
  }
}

function asErrorBody(value: unknown): { code: StableErrorCode; message: string } {
  if (value && typeof value === "object" && "code" in value) {
    const record = value as Record<string, unknown>;
    return {
      code: String(record.code),
      message: typeof record.message === "string" ? record.message : "request failed",
    };
  }
  return { code: "INTERNAL", message: "request failed" };
}

function parseSSEChunk(chunk: string): AgentEvent | null {
  let type = "";
  let data = "";
  for (const line of chunk.split("\n")) {
    if (line.startsWith("event:")) {
      type = line.slice("event:".length).trim();
    }
    if (line.startsWith("data:")) {
      data += line.slice("data:".length).trim();
    }
  }
  if (!type || !data) {
    return null;
  }
  try {
    return { type, data: JSON.parse(data) as unknown };
  } catch {
    return null;
  }
}
