import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "@/lib/api";
import { useAccounts } from "@/hooks/useAccounts";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ErrorState, PageLoader } from "@/components/ui/spinner";
import { formatCurrency } from "@/lib/utils";

export default function TransferPage() {
  const { data: accounts, isLoading, isError } = useAccounts();
  const queryClient = useQueryClient();

  const [fromAccount, setFromAccount] = useState("");
  const [toAccount, setToAccount] = useState("");
  const [amount, setAmount] = useState("");
  const [description, setDescription] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: (data: {
      from_account: string;
      to_account: string;
      amount_cents: number;
      description?: string;
    }) => api.transfer(data, crypto.randomUUID()),
    onSuccess: (txn) => {
      setSuccess(
        `Transferencia de ${formatCurrency(txn.amount_cents)} a ${txn.to_account} realizada.`,
      );
      setAmount("");
      setDescription("");
      setToAccount("");
      void queryClient.invalidateQueries({ queryKey: ["accounts"] });
      void queryClient.invalidateQueries({ queryKey: ["account"] });
      void queryClient.invalidateQueries({ queryKey: ["transactions"] });
    },
    onError: (err) => {
      setError(err instanceof ApiError ? err.message : "No se pudo realizar la transferencia");
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
    mutation.mutate({ from_account: fromAccount, to_account: toAccount, amount_cents: cents, description });
  };

  if (isLoading) return <PageLoader />;
  if (isError) return <ErrorState message="No se pudieron cargar tus cuentas." />;

  return (
    <div className="mx-auto max-w-lg space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Transferir</h1>
        <p className="text-sm text-muted-foreground">Envía dinero a otra cuenta</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-lg">Nueva transferencia</CardTitle>
          <CardDescription>La transferencia se registra en tu historial</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="from">Cuenta de origen</Label>
              <select
                id="from"
                className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                value={fromAccount}
                onChange={(e) => setFromAccount(e.target.value)}
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
              Transferir
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
