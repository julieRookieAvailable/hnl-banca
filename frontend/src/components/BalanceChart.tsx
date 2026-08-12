import { useId } from "react";
import { formatCurrency } from "@/lib/utils";
import type { Transaction } from "@/lib/api";

const W = 620;
const H = 230;
const BASELINE = 150;
const MAX_BAR = 120;

const POSITIVE = "#059669";
const NEGATIVE = "#dc2626";
const ZERO = "#d1d5db";

interface BalanceChartProps {
  transactions: Transaction[];
  userAccounts: string[];
}

export default function BalanceChart({ transactions, userAccounts }: BalanceChartProps) {
  const gradId = useId();

  if (transactions.length === 0) {
    return <p className="text-sm text-muted-foreground">No hay movimientos para graficar.</p>;
  }

  const owned = new Set(userAccounts);
  const values = transactions.map((t) => {
    if (owned.has(t.from_account)) return -t.amount_cents;
    if (owned.has(t.to_account)) return t.amount_cents;
    return 0;
  });

  const maxAbs = Math.max(1, ...values.map((v) => Math.abs(v)));
  const n = values.length;
  const gap = 4;
  const barW = Math.max(3, (W - 24 - gap * (n - 1)) / n);
  const x0 = 12;

  return (
    <div className="space-y-2">
      <svg
        viewBox={`0 0 ${W} ${H}`}
        className="h-48 w-full"
        role="img"
        aria-label="Gráfica de movimientos recientes"
      >
        <defs>
          <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#10b981" stopOpacity="0.35" />
            <stop offset="100%" stopColor="#10b981" stopOpacity="0" />
          </linearGradient>
        </defs>

        <line x1={x0} y1={BASELINE} x2={W - 12} y2={BASELINE} stroke="#d1d5db" strokeWidth="1" />
        <text x={W - 12} y={BASELINE + 16} textAnchor="end" className="fill-muted-foreground text-[10px]">
          0
        </text>

        <rect x={x0} y={12} width={W - 24} height={BASELINE - 12} fill={`url(#${gradId})`} />

        {values.map((v, i) => {
          const x = x0 + i * (barW + gap);
          const abs = Math.abs(v);
          const h = (abs / maxAbs) * MAX_BAR;
          const color = v > 0 ? POSITIVE : v < 0 ? NEGATIVE : ZERO;
          const t = transactions[i];
          return (
            <g key={t.id}>
              <rect
                x={x}
                y={v >= 0 ? BASELINE - h : BASELINE}
                width={barW}
                height={Math.max(1, h)}
                rx="2"
                fill={color}
                opacity="0.9"
              >
                <title>
                  {t.description || t.type}: {v > 0 ? "+" : ""}
                  {formatCurrency(v)}
                </title>
              </rect>
            </g>
          );
        })}
      </svg>

      <div className="flex flex-wrap items-center gap-4 text-xs text-muted-foreground">
        <span className="inline-flex items-center gap-1.5">
          <span className="h-2.5 w-2.5 rounded-sm" style={{ backgroundColor: POSITIVE }} />
          Ingresos (depósitos, transferencias recibidas)
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span className="h-2.5 w-2.5 rounded-sm" style={{ backgroundColor: NEGATIVE }} />
          Egresos (retiros, transferencias enviadas)
        </span>
      </div>
    </div>
  );
}
