import { describe, expect, it, vi } from "vitest";
import type { Order } from "../api/client";
import { submitWalletPayment } from "./payment";

describe("submitWalletPayment", () => {
  it("replays order status after dependency timeout", async () => {
    const order = paidOrder();
    const payWallet = vi.fn().mockRejectedValue({ code: "DEPENDENCY_TIMEOUT" });
    const getOrder = vi.fn().mockResolvedValue(order);

    const result = await submitWalletPayment("order_1", { payWallet, getOrder });

    expect(getOrder).toHaveBeenCalledWith("order_1");
    expect(result).toEqual({ state: "replayed", order, errorCode: "DEPENDENCY_TIMEOUT" });
  });

  it("replays order status after PAYMENT_IN_PROGRESS", async () => {
    const order = pendingOrder();
    const payWallet = vi.fn().mockRejectedValue({ code: "PAYMENT_IN_PROGRESS" });
    const getOrder = vi.fn().mockResolvedValue(order);

    const result = await submitWalletPayment("order_1", { payWallet, getOrder });

    expect(result.state).toBe("replayed");
  });

  it("preserves order snapshot for out-of-stock failure", async () => {
    const current = pendingOrder();
    const payWallet = vi.fn().mockRejectedValue({ code: "OUT_OF_STOCK" });

    const result = await submitWalletPayment("order_1", { payWallet, getOrder: vi.fn(), currentOrder: current });

    expect(result).toEqual({ state: "blocked", order: current, errorCode: "OUT_OF_STOCK" });
  });
});

function paidOrder(): Order {
  return { ...pendingOrder(), status: "PAID", paid_amount: "4999.00" };
}

function pendingOrder(): Order {
  return {
    order_no: "order_1",
    request_id: "req_1",
    status: "PENDING_PAYMENT",
    total_amount: "4999.00",
    paid_amount: "0.00",
    items: [],
  };
}
