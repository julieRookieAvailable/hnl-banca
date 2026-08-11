import { useMutation } from "@tanstack/react-query";
import { api, type ChatAction } from "@/lib/api";

export function useChat() {
  return useMutation({
    mutationFn: ({
      message,
      history,
    }: {
      message: string;
      history: { role: string; content: string }[];
    }) => api.chat(message, history),
  });
}

export function useChatConfirm() {
  return useMutation({
    mutationFn: (pendingId: string) => api.chatConfirm(pendingId),
  });
}

export function useChatCancel() {
  return useMutation({
    mutationFn: (pendingId: string) => api.chatCancel(pendingId),
  });
}

export function describeAction(action: ChatAction | null): string | null {
  if (!action) return null;
  switch (action.type) {
    case "get_balances":
      return "Consulta de saldos";
    case "create_pending":
      return `Transferencia de ${action.from_account} a ${action.to_account} por ${action.amount_cents} centavos`;
    case "confirm_transfer":
      return "Confirmación de transferencia pendiente";
    case "cancel_transfer":
      return "Cancelación de transferencia pendiente";
    default:
      return action.type;
  }
}
