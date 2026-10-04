import { Badge } from "@/components/ui/badge";
import type { Row } from "@/lib/api";
import { shortEpic } from "@/lib/status";

export function ItemBadges({ row, showEpic = true }: { row: Row; showEpic?: boolean }) {
  const blockers = [...(row.blocked_by ?? []), ...(row.conditions ?? [])];
  return (
    <div className="flex flex-wrap gap-1">
      {row.type === "bug" && <Badge variant="destructive">bug</Badge>}
      {row.blocker && <Badge variant="destructive">blocker</Badge>}
      {row.size && <Badge variant="secondary">{row.size}</Badge>}
      {showEpic && row.epic && <Badge variant="outline">{shortEpic(row.epic)}</Badge>}
      {!row.epic && <Badge variant="outline">{row.type}</Badge>}
      {(row.labels ?? []).map((l) => (
        <Badge key={l} variant="outline">
          {l}
        </Badge>
      ))}
      {row.status !== "done" && row.status !== "dropped" && blockers.length > 0 && (
        <Badge variant="outline" className="border-amber-500/50 text-amber-700 dark:text-amber-400" title={blockers.join(", ")}>
          blocked
        </Badge>
      )}
      {row.status !== "done" && row.status !== "dropped" && blockers.length === 0 && row.ready && (
        <Badge variant="outline" className="border-emerald-500/50 text-emerald-700 dark:text-emerald-400">
          ready
        </Badge>
      )}
    </div>
  );
}
