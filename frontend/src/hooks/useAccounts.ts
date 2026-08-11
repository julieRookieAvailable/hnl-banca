import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

export function useAccounts() {
  return useQuery({
    queryKey: ["accounts"],
    queryFn: api.accounts,
  });
}

export function useAccount(accountNumber: string) {
  return useQuery({
    queryKey: ["account", accountNumber],
    queryFn: () => api.account(accountNumber),
    enabled: !!accountNumber,
  });
}

const PAGE_SIZE = 10;

export function useTransactions(accountNumber: string, page: number) {
  const offset = (page - 1) * PAGE_SIZE;
  return useQuery({
    queryKey: ["transactions", accountNumber, page],
    queryFn: () => api.transactions(accountNumber, PAGE_SIZE, offset),
    enabled: !!accountNumber,
  });
}

export function useRecentTransactions() {
  return useQuery({
    queryKey: ["transactions", "recent"],
    queryFn: api.recentTransactions,
  });
}

export { PAGE_SIZE };
