import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiFetch, clearToken, opsFetch, setToken } from "./client";

describe("apiFetch", () => {
  beforeEach(() => {
    clearToken();
  });

  it("attaches bearer token and parses JSON responses", async () => {
    setToken("jwt-token");
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({ products: [] }), { status: 200 }));

    const result = await apiFetch("/api/v1/products", {}, fetcher);

    expect(fetcher).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/products",
      expect.objectContaining({
        headers: expect.objectContaining({ Authorization: "Bearer jwt-token" }),
      }),
    );
    expect(result).toEqual({ products: [] });
  });

  it("maps stable Gateway errors without leaking raw messages", async () => {
    const fetcher = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ code: "OUT_OF_STOCK", message: "requested inventory is unavailable" }), {
        status: 409,
      }),
    );

    await expect(apiFetch("/api/v1/orders/o/payments/wallet", {}, fetcher)).rejects.toMatchObject({
      code: "OUT_OF_STOCK",
      message: "requested inventory is unavailable",
      status: 409,
    });
  });

  it("maps aborted requests to REQUEST_TIMEOUT", async () => {
    const fetcher = vi.fn().mockRejectedValue(new DOMException("aborted", "AbortError"));

    await expect(apiFetch("/api/v1/orders", {}, fetcher)).rejects.toMatchObject({
      code: "REQUEST_TIMEOUT",
    });
  });

  it("sends operator header only through opsFetch", async () => {
    setToken("jwt-token");
    const normalFetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({ ok: true }), { status: 200 }));
    await apiFetch("/api/v1/products", {}, normalFetcher);
    expect(normalFetcher).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/products",
      expect.objectContaining({
        headers: expect.not.objectContaining({ "X-AI-Shopping-Operator": "true" }),
      }),
    );

    const opsFetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({ documents: [] }), { status: 200 }));
    await opsFetch("/api/v1/ops/knowledge/documents", {}, opsFetcher);
    expect(opsFetcher).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/ops/knowledge/documents",
      expect.objectContaining({
        headers: expect.objectContaining({ "X-AI-Shopping-Operator": "true", Authorization: "Bearer jwt-token" }),
      }),
    );
  });
});
