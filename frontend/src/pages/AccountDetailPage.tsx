import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { ChevronLeft, ChevronRight, Download } from "lucide-react";
import { useAccount, useTransactions, PAGE_SIZE } from "@/hooks/useAccounts";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { ErrorState, PageLoader } from "@/components/ui/spinner";
import { formatCurrency, formatDate } from "@/lib/utils";
import { cn } from "@/lib/utils";
import { api } from "@/lib/api";

export default function AccountDetailPage() {
  const { accountNumber = "" } = useParams();
  const [page, setPage] = useState(1);
  const [exporting, setExporting] = useState(false);

  const account = useAccount(accountNumber);
  const txns = useTransactions(accountNumber, page);

  const rows = txns.data ?? [];
  const hasNext = rows.length === PAGE_SIZE;

  const handleExport = async () => {
    try {
      setExporting(true);
      const blob = await api.transactionsCsv(accountNumber);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `movimientos-${accountNumber}.csv`;
      a.click();
      URL.revokeObjectURL(url);
    } catch {
      // el error se muestra silenciosamente en el botón
    } finally {
      setExporting(false);
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <Link
          to="/"
          className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
        >
          <ChevronLeft className="h-4 w-4" /> Volver
        </Link>
      </div>

      {account.isLoading && <PageLoader />}
      {account.isError && <ErrorState message="No se pudo cargar la cuenta." />}

      {account.data && (
        <Card>
          <CardHeader className="pb-2">
            <CardDescription className="font-mono text-sm">
              {account.data.account_number}
            </CardDescription>
            <CardTitle>{formatCurrency(account.data.balance_cents)}</CardTitle>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">
            Saldo disponible al momento
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-lg">Movimientos</CardTitle>
          <div className="mt-1">
            <Button
              variant="outline"
              size="sm"
              onClick={handleExport}
              disabled={exporting}
            >
              {exporting ? "Exportando…" : (
                <>
                  <Download className="mr-1 h-4 w-4" /> Exportar CSV
                </>
              )}
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {txns.isLoading && <PageLoader />}
          {txns.isError && <ErrorState message="No se pudieron cargar los movimientos." />}

          {rows.length === 0 && !txns.isLoading && !txns.isError && (
            <p className="text-sm text-muted-foreground">No hay movimientos registrados.</p>
          )}

          {rows.length > 0 && (
            <div className="divide-y">
              {rows.map((t) => {
                const isDebit = t.type === "debit" || t.type === "withdrawal";
                const isCredit = t.type === "credit" || t.type === "deposit";
                return (
                  <div key={t.id} className="flex items-center justify-between gap-4 py-3">
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium">{t.description}</p>
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
                      <Badge variant={t.status === "posted" ? "success" : "secondary"}>
                        {t.status}
                      </Badge>
                    </div>
                  </div>
                );
              })}
            </div>
          )}

          {rows.length > 0 && (
            <div className="mt-4 flex items-center justify-between">
              <Button
                variant="outline"
                size="sm"
                disabled={page === 1}
                onClick={() => setPage((p) => p - 1)}
              >
                <ChevronLeft className="h-4 w-4" /> Anterior
              </Button>
              <span className="text-sm text-muted-foreground">Página {page}</span>
              <Button variant="outline" size="sm" disabled={!hasNext} onClick={() => setPage((p) => p + 1)}>
                Siguiente <ChevronRight className="h-4 w-4" />
              </Button>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
