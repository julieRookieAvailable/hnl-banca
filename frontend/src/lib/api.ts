export const API_URL = import.meta.env.VITE_API_URL ?? "http://localhost:8080";

export class ApiError extends Error {
  status: number;
  code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

type ApiErrorBody = { error?: { code?: string; message?: string } };

export interface Tokens {
  access_token: string;
  refresh_token: string;
  expires_in: number;
}

export interface User {
  id: string;
  email: string;
  full_name: string;
}

export interface LoginResponse {
  user: User;
  tokens: Tokens;
}

export interface Account {
  account_number: string;
  account_type: "checking" | "savings" | "investment";
  currency: string;
  balance_cents: number;
}

export interface Transaction {
  id: number;
  from_account: string;
  to_account: string;
  type: string;
  amount_cents: number;
  description: string;
  timestamp: string;
  status: string;
}

export interface ChatAction {
  type: string;
  pending_id?: string;
  from_account?: string;
  to_account?: string;
  amount_cents?: number;
  description?: string;
}

export interface ChatResponse {
  reply: string;
  action: ChatAction | null;
}

const TOKEN_KEY = "hnl_access_token";
const REFRESH_KEY = "hnl_refresh_token";

export function getAccessToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function getRefreshToken(): string | null {
  return localStorage.getItem(REFRESH_KEY);
}

export function storeTokens(tokens: Tokens): void {
  localStorage.setItem(TOKEN_KEY, tokens.access_token);
  localStorage.setItem(REFRESH_KEY, tokens.refresh_token);
}

export function clearTokens(): void {
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(REFRESH_KEY);
}

async function request<T>(
  path: string,
  options: RequestInit = {},
  withAuth = false,
): Promise<T> {
  const headers = new Headers(options.headers);
  headers.set("Content-Type", "application/json");
  if (withAuth) {
    const token = getAccessToken();
    if (token) {
      headers.set("Authorization", `Bearer ${token}`);
    }
  }

  const res = await fetch(`${API_URL}${path}`, { ...options, headers });

  const raw = await res.text();
  let body: unknown = null;
  if (raw) {
    try {
      body = JSON.parse(raw);
    } catch {
      body = raw;
    }
  }

  if (!res.ok) {
    const err = (body as ApiErrorBody)?.error;
    throw new ApiError(res.status, err?.code ?? "UNKNOWN", err?.message ?? `Error ${res.status}`);
  }

  return body as T;
}

export const api = {
  register: (data: { email: string; password: string; full_name: string }) =>
    request<LoginResponse>("/auth/register", { method: "POST", body: JSON.stringify(data) }),
  login: (data: { email: string; password: string }) =>
    request<LoginResponse>("/auth/login", { method: "POST", body: JSON.stringify(data) }),
  refresh: (refresh_token: string) =>
    request<{ access_token: string; refresh_token: string; expires_in: number }>("/auth/refresh", {
      method: "POST",
      body: JSON.stringify({ refresh_token }),
    }),
  logout: (refresh_token: string) =>
    request<void>("/auth/logout", { method: "POST", body: JSON.stringify({ refresh_token }) }),

  accounts: () => request<Account[]>("/accounts", {}, true),
  account: (accountNumber: string) =>
    request<Account>(`/accounts/${encodeURIComponent(accountNumber)}`, {}, true),
  transactions: (accountNumber: string, limit: number, offset: number) =>
    request<Transaction[]>(
      `/accounts/${encodeURIComponent(accountNumber)}/transactions?limit=${limit}&offset=${offset}`,
      {},
      true,
    ),
  transfer: (data: {
    from_account: string;
    to_account: string;
    amount_cents: number;
    description?: string;
  }, idempotencyKey?: string) =>
    request<Transaction>("/transfers", {
      method: "POST",
      body: JSON.stringify(data),
      headers: idempotencyKey ? { "Idempotency-Key": idempotencyKey } : {},
    }, true),

  chat: (message: string, history: { role: string; content: string }[]) =>
    request<ChatResponse>("/chat", { method: "POST", body: JSON.stringify({ message, history }) }, true),
  chatConfirm: (pending_id: string) =>
    request<{ status: string }>("/chat/confirm", { method: "POST", body: JSON.stringify({ pending_id }) }, true),
  chatCancel: (pending_id: string) =>
    request<{ status: string }>("/chat/cancel", { method: "POST", body: JSON.stringify({ pending_id }) }, true),
};
