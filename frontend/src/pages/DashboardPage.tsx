import { Link } from "react-router-dom";
import { ArrowRight } from "lucide-react";
import { useAccounts, useRecentTransactions } from "@/hooks/useAccounts";
import { useAuth } from "@/context/auth";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { ErrorState, PageLoader } from "@/components/ui/spinner";
import { formatCurrency, formatDate } from "@/lib/utils";
import { cn } from "@/lib/utils";

const typeLabels: Record<string, string> = {
  checking: "Cheques",
  savings: "Ahorros",
  investment: "Inversión",
};

export default function DashboardPage() {
  const { user } = useAuth();
  const { data, isLoading, isError } = useAccounts();
  const recent = useRecentTransactions();

  const total = data?.reduce((sum, a) => sum + a.balance_cents, 0) ?? 0;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Hola, {user?.full_name}</h1>
        <p className="text-sm text-muted-foreground">
          Tu saldo total es{" "}
          <span className="font-semibold text-foreground">{formatCurrency(total)}</span>
        </p>
      </div>

      {isLoading && <PageLoader />}
      {isError && <ErrorState message="No se pudieron cargar tus cuentas." />}

      {data && data.length === 0 && (
        <Card>
          <CardContent className="pt-6">
            <p className="text-sm text-muted-foreground">Aún no tienes cuentas abiertas.</p>
          </CardContent>
        </Card>
      )}

      {data && data.length > 0 && (
        <div className="grid gap-4 sm:grid-cols-2">
          {data.map((account) => (
            <Link key={account.account_number} to={`/cuentas/${account.account_number}`}>
              <Card className="h-full transition-colors hover:border-primary/50">
                <CardHeader className="pb-2">
                  <div className="flex items-center justify-between">
                    <CardDescription className="font-mono text-sm">
                      {account.account_number}
                    </CardDescription>
                    <Badge variant="secondary">{typeLabels[account.account_type] ?? account.account_type}</Badge>
                  </div>
                  <CardTitle className="text-2xl">
                    {formatCurrency(account.balance_cents)}
                  </CardTitle>
                </CardHeader>
                <CardContent className="flex items-center justify-between text-sm text-muted-foreground">
                  <span>{account.currency}</span>
                  <span className="inline-flex items-center gap-1 font-medium text-primary">
                    Ver movimientos <ArrowRight className="h-4 w-4" />
                  </span>
                </CardContent>
              </Card>
            </Link>
          ))}
        </div>
      )}

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-lg">Movimientos recientes</CardTitle>
          <CardDescription>Últimas operaciones de tus cuentas</CardDescription>
        </CardHeader>
        <CardContent>
          {recent.isLoading && <PageLoader />}
          {recent.isError && <ErrorState message="No se pudieron cargar los movimientos recientes." />}

          {recent.data && recent.data.length === 0 && (
            <p className="text-sm text-muted-foreground">Aún no hay movimientos registrados.</p>
          )}

          {recent.data && recent.data.length > 0 && (
            <div className="divide-y">
              {recent.data.map((t) => {
                const isDebit = t.type === "debit" || t.type === "withdrawal";
                const isCredit = t.type === "credit" || t.type === "deposit";
                return (
                  <div key={t.id} className="flex items-center justify-between gap-4 py-3">
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium">{t.description || t.type}</p>
                      <p className="text-xs text-muted-foreground">{formatDate(t.timestamp)}</p>
                      <p className="mt-0.5 font-mono text-xs text-muted-foreground">
                        {t.from_account} → {t.to_account}
                      </p>
                    </div>
                    <div className="flex flex-col items-end gap-1">
                      <span
                        className={cn(
                          "text-sm font-semibold",
                          isCredit ? "text-emerald-600" : isDebit ? "text-destructive" : "",
                        )}
                      >
                        {isDebit ? "-" : "+"}{formatCurrency(t.amount_cents)}
                      </span>
                      <Badge variant={t.status === "posted" || t.status === "completed" ? "success" : "secondary"}>
                        {t.status}
                      </Badge>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
