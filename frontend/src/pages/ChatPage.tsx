import { useState, useRef, useEffect } from "react";
import { Send, Bot, User, Check, X } from "lucide-react";
import { useChat, useChatConfirm, useChatCancel, describeAction } from "@/hooks/useChat";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { formatCurrency } from "@/lib/utils";
import type { ChatAction } from "@/lib/api";

interface Message {
  role: "user" | "assistant";
  content: string;
  action?: ChatAction | null;
  actionStatus?: "pending" | "done" | "cancelled";
}

export default function ChatPage() {
  const [messages, setMessages] = useState<Message[]>([
    {
      role: "assistant",
      content:
        "Hola, soy tu asistente financiero. Puedo consultar tus saldos y ayudarte a realizar transferencias. ¿Qué deseas hacer?",
    },
  ]);
  const [input, setInput] = useState("");
  const bottomRef = useRef<HTMLDivElement>(null);

  const chat = useChat();
  const confirm = useChatConfirm();
  const cancel = useChatCancel();

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  const send = async (text?: string) => {
    const content = (text ?? input).trim();
    if (!content || chat.isPending) return;
    setInput("");
    const history = messages.map((m) => ({ role: m.role, content: m.content }));
    setMessages((prev) => [...prev, { role: "user", content }]);
    try {
      const res = await chat.mutateAsync({ message: content, history });
      setMessages((prev) => [
        ...prev,
        { role: "assistant", content: res.reply, action: res.action, actionStatus: res.action ? "pending" : undefined },
      ]);
    } catch {
      setMessages((prev) => [
        ...prev,
        { role: "assistant", content: "Lo siento, no pude procesar tu solicitud." },
      ]);
    }
  };

  const handleAction = async (index: number, action: ChatAction | null, confirmIt: boolean) => {
    if (!action?.pending_id) return;
    try {
      if (confirmIt) {
        await confirm.mutateAsync(action.pending_id);
        setMessages((prev) =>
          prev.map((m, i) => (i === index ? { ...m, actionStatus: "done" } : m)),
        );
      } else {
        await cancel.mutateAsync(action.pending_id);
        setMessages((prev) =>
          prev.map((m, i) => (i === index ? { ...m, actionStatus: "cancelled" } : m)),
        );
      }
    } catch {
      setMessages((prev) =>
        prev.map((m, i) =>
          i === index
            ? { ...m, content: m.content + " (No se pudo procesar la acción.)" }
            : m,
        ),
      );
    }
  };

  return (
    <div className="mx-auto max-w-2xl space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Asistente</h1>
        <p className="text-sm text-muted-foreground">
          Consulta saldos y haz transferencias en lenguaje natural
        </p>
      </div>

      <Card className="flex h-[60vh] flex-col">
        <CardHeader className="border-b pb-3">
          <CardTitle className="flex items-center gap-2 text-base">
            <Bot className="h-5 w-5 text-primary" /> BancaBot
          </CardTitle>
        </CardHeader>
        <CardContent className="flex-1 space-y-4 overflow-y-auto">
          {messages.map((m, i) => (
            <div key={i} className={`flex ${m.role === "user" ? "justify-end" : "justify-start"}`}>
              <div
                className={`max-w-[85%] rounded-lg px-4 py-2 text-sm ${
                  m.role === "user"
                    ? "bg-primary text-primary-foreground"
                    : "bg-secondary text-secondary-foreground"
                }`}
              >
                <div className="mb-1 flex items-center gap-1.5">
                  {m.role === "assistant" ? (
                    <Bot className="h-3.5 w-3.5" />
                  ) : (
                    <User className="h-3.5 w-3.5" />
                  )}
                </div>
                <p className="whitespace-pre-wrap">{m.content}</p>

                {m.action && m.action.type === "create_pending" && m.actionStatus === "pending" && (
                  <div className="mt-3 space-y-3 rounded-md border bg-background p-3">
                    <div className="text-xs text-muted-foreground">
                      <p>
                        De <span className="font-mono">{m.action.from_account}</span> a{" "}
                        <span className="font-mono">{m.action.to_account}</span>
                      </p>
                      <p className="mt-1 font-semibold text-foreground">
                        {formatCurrency(m.action.amount_cents ?? 0)}
                      </p>
                      {m.action.description && <p className="mt-1">{m.action.description}</p>}
                    </div>
                    <div className="flex gap-2">
                      <Button
                        size="sm"
                        className="flex-1"
                        onClick={() => handleAction(i, m.action ?? null, true)}
                        loading={confirm.isPending}
                      >
                        <Check className="h-4 w-4" /> Confirmar
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        className="flex-1"
                        onClick={() => handleAction(i, m.action ?? null, false)}
                        loading={cancel.isPending}
                      >
                        <X className="h-4 w-4" /> Cancelar
                      </Button>
                    </div>
                  </div>
                )}

                {m.action && m.actionStatus === "done" && (
                  <Badge className="mt-2" variant="success">
                    Transferencia confirmada
                  </Badge>
                )}
                {m.action && m.actionStatus === "cancelled" && (
                  <Badge className="mt-2" variant="secondary">
                    Transferencia cancelada
                  </Badge>
                )}

                {m.action && m.action.type !== "create_pending" && (
                  <p className="mt-2 text-xs text-muted-foreground">
                    {describeAction(m.action)}
                  </p>
                )}
              </div>
            </div>
          ))}
          {chat.isPending && (
            <div className="flex justify-start">
              <div className="rounded-lg bg-secondary px-4 py-2 text-sm">Escribiendo…</div>
            </div>
          )}
          <div ref={bottomRef} />
        </CardContent>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void send();
          }}
          className="flex gap-2 border-t p-3"
        >
          <Input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="Ej. ¿Cuál es mi saldo? o transfiere 100 a 0000-0000-0000-0000"
          />
          <Button type="submit" size="icon" disabled={chat.isPending}>
            <Send className="h-4 w-4" />
          </Button>
        </form>
      </Card>
    </div>
  );
}
