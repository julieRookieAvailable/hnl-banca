import { useId, useRef, useState } from "react";
import { formatCurrency } from "@/lib/utils";
import type { Transaction } from "@/lib/api";

const W = 620;
const H = 230;
const BASELINE = 150;
const MAX_BAR = 120;

const ZERO = "#d1d5db";

interface BalanceChartProps {
  transactions: Transaction[];
  userAccounts: string[];
}

export default function BalanceChart({ transactions, userAccounts }: BalanceChartProps) {
  const gradId = useId();
  const containerRef = useRef<HTMLDivElement>(null);
  const [hover, setHover] = useState<number | null>(null);
  const [pos, setPos] = useState({ x: 0, y: 0, w: 0 });

  const owned = new Set(userAccounts);
  const chartTxs = transactions.filter((t) => {
    const fromOwned = owned.has(t.from_account);
    const toOwned = owned.has(t.to_account);
    return !(fromOwned && toOwned);
  });

  if (chartTxs.length === 0) {
    return <p className="text-sm text-muted-foreground">No hay movimientos para graficar.</p>;
  }

  const values = chartTxs.map((t) => {
    if (owned.has(t.from_account)) return -t.amount_cents;
    if (owned.has(t.to_account)) return t.amount_cents;
    return 0;
  });

  const maxAbs = Math.max(1, ...values.map((v) => Math.abs(v)));
  const n = values.length;
  const gap = 4;
  const barW = Math.max(3, (W - 24 - gap * (n - 1)) / n);
  const x0 = 12;

  const handleMove = (e: React.MouseEvent) => {
    const rect = containerRef.current?.getBoundingClientRect();
    if (!rect) return;
    setPos({ x: e.clientX - rect.left, y: e.clientY - rect.top, w: rect.width });
  };

  const tooltip = hover !== null ? chartTxs[hover] : null;

  return (
    <div ref={containerRef} className="relative space-y-2">
      <svg
        viewBox={`0 0 ${W} ${H}`}
        className="h-48 w-full"
        role="img"
        aria-label="Gráfica de movimientos recientes"
        onMouseLeave={() => setHover(null)}
      >
        <defs>
          <linearGradient id={`${gradId}-bg`} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#10b981" stopOpacity="0.35" />
            <stop offset="100%" stopColor="#10b981" stopOpacity="0" />
          </linearGradient>
          <linearGradient id={`${gradId}-pos`} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#34d399" />
            <stop offset="100%" stopColor="#059669" />
          </linearGradient>
          <linearGradient id={`${gradId}-neg`} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#f87171" />
            <stop offset="100%" stopColor="#dc2626" />
          </linearGradient>
        </defs>

        <line x1={x0} y1={BASELINE} x2={W - 12} y2={BASELINE} stroke="#d1d5db" strokeWidth="1" />
        <text x={W - 12} y={BASELINE + 16} textAnchor="end" className="fill-muted-foreground text-[10px]">
          0
        </text>

        <rect x={x0} y={12} width={W - 24} height={BASELINE - 12} fill={`url(#${gradId}-bg)`} />

        {values.map((v, i) => {
          const x = x0 + i * (barW + gap);
          const abs = Math.abs(v);
          const h = (abs / maxAbs) * MAX_BAR;
          const fill = v > 0 ? `url(#${gradId}-pos)` : v < 0 ? `url(#${gradId}-neg)` : ZERO;
          const t = chartTxs[i];
          return (
            <g key={t.id}>
              <rect
                x={x}
                y={v >= 0 ? BASELINE - h : BASELINE}
                width={barW}
                height={Math.max(1, h)}
                rx="2"
                fill={fill}
                opacity="0.9"
                onMouseEnter={() => setHover(i)}
                onMouseMove={handleMove}
              />
            </g>
          );
        })}
      </svg>

      {tooltip && (
        <div
          className="pointer-events-none absolute z-10 rounded-md border bg-popover px-2.5 py-1.5 text-xs shadow-lg"
          style={{
            left: Math.min(Math.max(pos.x + 12, 4), Math.max(4, pos.w - 220)),
            top: Math.max(pos.y - 44, 4),
          }}
        >
          <p className="max-w-[220px] truncate font-medium">{tooltip.description || tooltip.type}</p>
          <p className="text-muted-foreground">
            {formatCurrency(values[hover!])} · {tooltip.type}
          </p>
        </div>
      )}

      <div className="flex flex-wrap items-center gap-4 text-xs text-muted-foreground">
        <span className="inline-flex items-center gap-1.5">
          <span
            className="h-2.5 w-2.5 rounded-sm"
            style={{ background: "linear-gradient(to bottom, #34d399, #059669)" }}
          />
          Ingresos (depósitos, transferencias recibidas)
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span
            className="h-2.5 w-2.5 rounded-sm"
            style={{ background: "linear-gradient(to bottom, #f87171, #dc2626)" }}
          />
          Egresos (retiros)
        </span>
      </div>
      <p className="text-xs text-muted-foreground">
        Las transferencias entre tus propias cuentas no se cuentan como ingresos ni egresos.
      </p>
    </div>
  );
}
