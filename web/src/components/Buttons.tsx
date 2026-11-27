import { TriangleAlert } from "lucide-react";
import type { ReactNode } from "react";

export function ActionButton({
  children,
  onClick,
}: {
  children: ReactNode;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="inline-flex items-center gap-1.5 rounded-lg border border-slate-700 px-3 py-1.5 text-xs text-slate-200 hover:border-cyan-400 hover:text-cyan-300"
    >
      {children}
    </button>
  );
}

export function DangerButton({
  children,
  onClick,
}: {
  children: ReactNode;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="inline-flex items-center gap-1.5 rounded-lg border border-rose-900/80 px-3 py-1.5 text-xs text-rose-300 hover:bg-rose-950/50"
    >
      <TriangleAlert size={14} />
      {children}
    </button>
  );
}
