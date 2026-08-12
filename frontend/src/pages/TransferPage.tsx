import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "@/lib/api";
import { useAccounts } from "@/hooks/useAccounts";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent } from "@/components/ui/card";
import { ErrorState, PageLoader } from "@/components/ui/spinner";
import { formatCurrency } from "@/lib/utils";
import { cn } from "@/lib/utils";

type Op = "deposit" | "withdraw" | "transfer";

const OPS: { id: Op; label: string }[] = [
  { id: "deposit", label: "Depósito" },
  { id: "withdraw", label: "Retiro" },
  { id: "transfer", label: "Transferencia" },
];

const TITLES: Record<Op, { title: string; description: string }> = {
  deposit: {
    title: "Depositar",
    description: "Recibe dinero desde fuera del banco",
  },
  withdraw: {
    title: "Retirar",
    description: "Retira dinero de tu cuenta",
  },
  transfer: {
    title: "Transferir",
    description: "Envía dinero a otra cuenta",
  },
};

export default function TransferPage() {
  const { data: accounts, isLoading, isError } = useAccounts();
  const queryClient = useQueryClient();

  const [op, setOp] = useState<Op>("transfer");
  const [account, setAccount] = useState("");
  const [toAccount, setToAccount] = useState("");
  const [amount, setAmount] = useState("");
  const [description, setDescription] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: (data: {
      op: Op;
      account: string;
      toAccount: string;
      amountCents: number;
      description?: string;
    }) => {
      const key = crypto.randomUUID();
      const body = { account_number: data.account, amount_cents: data.amountCents, description: data.description };
      if (data.op === "deposit") return api.deposit(body, key);
      if (data.op === "withdraw") return api.withdraw(body, key);
      return api.transfer(
        { from_account: data.account, to_account: data.toAccount, amount_cents: data.amountCents, description: data.description },
        key,
      );
    },
    onSuccess: (txn) => {
      const verbs: Record<Op, string> = {
        deposit: `Depósito de ${formatCurrency(txn.amount_cents)} a tu cuenta realizado.`,
        withdraw: `Retiro de ${formatCurrency(txn.amount_cents)} de tu cuenta realizado.`,
        transfer: `Transferencia de ${formatCurrency(txn.amount_cents)} a ${txn.to_account} realizada.`,
      };
      setSuccess(verbs[op]);
      setAmount("");
      setDescription("");
      setToAccount("");
      void queryClient.invalidateQueries({ queryKey: ["accounts"] });
      void queryClient.invalidateQueries({ queryKey: ["account"] });
      void queryClient.invalidateQueries({ queryKey: ["transactions"] });
    },
    onError: (err) => {
      setError(err instanceof ApiError ? err.message : "No se pudo completar la operación");
    },
  });

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSuccess(null);
    const cents = Math.round(parseFloat(amount) * 100);
    if (!cents || cents <= 0) {
      setError("Ingresa un monto válido mayor a cero");
      return;
    }
    mutation.mutate({ op, account, toAccount, amountCents: cents, description });
  };

  const switchOp = (next: Op) => {
    setOp(next);
    setError(null);
    setSuccess(null);
    setToAccount("");
  };

  if (isLoading) return <PageLoader />;
  if (isError) return <ErrorState message="No se pudieron cargar tus cuentas." />;

  return (
    <div className="mx-auto max-w-lg space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">{TITLES[op].title}</h1>
        <p className="text-sm text-muted-foreground">{TITLES[op].description}</p>
      </div>

      <Card>
        <div className="p-4">
          <div className="flex gap-1 rounded-lg bg-muted p-1">
            {OPS.map((o) => (
              <button
                key={o.id}
                type="button"
                onClick={() => switchOp(o.id)}
                className={cn(
                  "flex-1 rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
                  op === o.id
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {o.label}
              </button>
            ))}
          </div>
        </div>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="account">
                {op === "transfer" ? "Cuenta de origen" : "Cuenta"}
              </Label>
              <select
                id="account"
                className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                value={account}
                onChange={(e) => setAccount(e.target.value)}
                required
              >
                <option value="">Selecciona una cuenta</option>
                {(accounts ?? []).map((a) => (
                  <option key={a.account_number} value={a.account_number}>
                    {a.account_number} — {formatCurrency(a.balance_cents)}
                  </option>
                ))}
              </select>
            </div>

            {op === "transfer" && (
              <div className="space-y-2">
                <Label htmlFor="to">Cuenta destino</Label>
                <Input
                  id="to"
                  value={toAccount}
                  onChange={(e) => setToAccount(e.target.value)}
                  placeholder="0000-0000-0000-0000"
                  required
                />
              </div>
            )}

            <div className="space-y-2">
              <Label htmlFor="amount">Monto (USD)</Label>
              <Input
                id="amount"
                type="number"
                min="0.01"
                step="0.01"
                value={amount}
                onChange={(e) => setAmount(e.target.value)}
                placeholder="0.00"
                required
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="description">Descripción (opcional)</Label>
              <Input
                id="description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Ej. Alquiler de enero"
              />
            </div>

            {error && <p className="text-sm text-destructive">{error}</p>}
            {success && <p className="text-sm text-emerald-600">{success}</p>}

            <Button type="submit" className="w-full" loading={mutation.isPending}>
              {TITLES[op].title}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
