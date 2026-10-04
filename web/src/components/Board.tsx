import { useState } from "react";
import { cn } from "@/lib/utils";
import type { Row, Status } from "@/lib/api";
import { BOARD_STATUSES, RECENT_DONE_DAYS, STATUS_DOT, STATUS_NAMES } from "@/lib/status";
import { ItemBadges } from "./ItemBadges";

interface Props {
  rows: Row[];
  showDone: boolean;
  hiddenDone: number; // done items older than the recent window, left out
  onShowAllDone: () => void;
  onOpen: (id: string) => void;
  onMove: (id: string, status: Status) => void;
}

export function Board({ rows, showDone, hiddenDone, onShowAllDone, onOpen, onMove }: Props) {
  const [over, setOver] = useState<Status | null>(null);
  const statuses = BOARD_STATUSES;
  return (
    <div className="grid min-h-0 flex-1 auto-cols-[minmax(17rem,24rem)] grid-flow-col justify-start gap-4 overflow-x-auto pb-2">
      {statuses.map((status) => {
        const items = rows.filter((r) => r.status === status);
        return (
          <section
            key={status}
            aria-label={STATUS_NAMES[status]}
            className={cn(
              "flex min-h-0 flex-col rounded-xl border bg-muted/40 transition-colors",
              over === status && "border-primary/60 bg-primary/5",
            )}
            onDragOver={(e) => {
              e.preventDefault();
              setOver(status);
            }}
            onDragLeave={() => setOver((s) => (s === status ? null : s))}
            onDrop={(e) => {
              e.preventDefault();
              setOver(null);
              const id = e.dataTransfer.getData("text/plain");
              const row = rows.find((r) => r.id === id);
              if (row && row.status !== status) onMove(id, status);
            }}
          >
            <header className="flex items-center gap-2 px-3 py-2.5 text-sm font-medium">
              <span className={cn("size-2 rounded-full", STATUS_DOT[status])} />
              {STATUS_NAMES[status]}
              <span className="ml-auto rounded-md bg-background px-1.5 text-xs tabular-nums text-muted-foreground">
                {items.length}
              </span>
            </header>
            <div className="flex min-h-24 flex-col gap-2 overflow-y-auto px-2 pb-2">
              {items.map((row) => (
                <article
                  key={row.id}
                  role="button"
                  aria-label={`${row.id}: ${row.title}`}
                  tabIndex={0}
                  draggable
                  onDragStart={(e) => e.dataTransfer.setData("text/plain", row.id)}
                  onClick={() => onOpen(row.id)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      onOpen(row.id);
                    }
                  }}
                  className="cursor-pointer rounded-lg border bg-card p-3 text-card-foreground shadow-xs transition hover:border-foreground/20 hover:shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  <div className="mb-1 font-mono text-xs text-muted-foreground">{row.id}</div>
                  <div className="mb-2 text-sm leading-snug">{row.title}</div>
                  <ItemBadges row={row} />
                </article>
              ))}
              {items.length === 0 && (
                <p className="rounded-lg border border-dashed px-3 py-6 text-center text-xs text-muted-foreground">
                  {status === "done" && !showDone ? `Nothing done in the last ${RECENT_DONE_DAYS} days` : "Drop a card here"}
                </p>
              )}
              {status === "done" && !showDone && hiddenDone > 0 && (
                <button
                  type="button"
                  onClick={onShowAllDone}
                  className="rounded-lg px-3 py-2 text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
                >
                  Showing the last {RECENT_DONE_DAYS} days · show {hiddenDone} older
                </button>
              )}
            </div>
          </section>
        );
      })}
    </div>
  );
}
