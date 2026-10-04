import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";
import type { Row } from "@/lib/api";
import { STATUS_DOT, STATUS_NAMES, shortEpic } from "@/lib/status";
import { ItemBadges } from "./ItemBadges";

export function ItemTable({ rows, onOpen }: { rows: Row[]; onOpen: (id: string) => void }) {
  return (
    <div className="min-h-0 flex-1 overflow-auto rounded-xl border">
      <Table>
        <TableHeader className="sticky top-0 bg-background">
          <TableRow>
            <TableHead className="w-28">ID</TableHead>
            <TableHead className="w-36">Status</TableHead>
            <TableHead>Title</TableHead>
            <TableHead className="w-28">Epic</TableHead>
            <TableHead className="w-56">Tags</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row) => (
            <TableRow
              key={row.id}
              tabIndex={0}
              className="cursor-pointer"
              onClick={() => onOpen(row.id)}
              onKeyDown={(e) => {
                if (e.key === "Enter") onOpen(row.id);
              }}
            >
              <TableCell className="font-mono text-xs text-muted-foreground">{row.id}</TableCell>
              <TableCell>
                <span className="inline-flex items-center gap-2 text-sm">
                  <span className={cn("size-2 rounded-full", STATUS_DOT[row.status])} />
                  {STATUS_NAMES[row.status]}
                </span>
              </TableCell>
              <TableCell className="max-w-0 truncate whitespace-nowrap" title={row.title}>
                {row.title}
              </TableCell>
              <TableCell className="text-sm text-muted-foreground">{row.epic ? shortEpic(row.epic) : row.type}</TableCell>
              <TableCell>
                <ItemBadges row={row} showEpic={false} />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
