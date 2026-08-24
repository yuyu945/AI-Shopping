import type { Order, StableErrorCode } from "../api/client";

type PaymentDeps = {
  payWallet: (orderNo: string) => Promise<Order>;
  getOrder: (orderNo: string) => Promise<Order>;
  currentOrder?: Order;
};

type PaymentResult =
  | { state: "paid"; order: Order }
  | { state: "replayed"; order: Order; errorCode: StableErrorCode }
  | { state: "blocked"; order?: Order; errorCode: StableErrorCode };

const replayCodes = new Set<StableErrorCode>([
  "DEPENDENCY_TIMEOUT",
  "NETWORK_ERROR",
  "REQUEST_TIMEOUT",
  "PAYMENT_IN_PROGRESS",
]);

const blockedCodes = new Set<StableErrorCode>(["OUT_OF_STOCK", "INSUFFICIENT_BALANCE"]);

export async function submitWalletPayment(orderNo: string, deps: PaymentDeps): Promise<PaymentResult> {
  try {
    const order = await deps.payWallet(orderNo);
    return { state: "paid", order };
  } catch (error) {
    const code = errorCode(error);
    if (replayCodes.has(code)) {
      const order = await deps.getOrder(orderNo);
      return { state: "replayed", order, errorCode: code };
    }
    if (blockedCodes.has(code)) {
      return { state: "blocked", order: deps.currentOrder, errorCode: code };
    }
    return { state: "blocked", order: deps.currentOrder, errorCode: code };
  }
}

function errorCode(error: unknown): StableErrorCode {
  if (error && typeof error === "object" && "code" in error) {
    return String((error as { code: unknown }).code);
  }
  return "INTERNAL";
}
