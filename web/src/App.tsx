import { useCallback, useEffect, useRef, useState } from "react";
import { LayoutGrid, List, Plus, Search } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { api, type Filters, type Meta, type Row, type Status } from "@/lib/api";
import { useItemRoute } from "@/lib/route";
import { STATUS_NAMES, recentlyDone } from "@/lib/status";
import { cn } from "@/lib/utils";
import { Board } from "@/components/Board";
import { FilterBar } from "@/components/FilterBar";
import { ItemSheet } from "@/components/ItemSheet";
import { ItemTable } from "@/components/ItemTable";
import { NewTaskDialog } from "@/components/NewTaskDialog";

type View = "board" | "list";

const EMPTY: Filters = { epic: "", type: "", label: "", size: "", text: "", ready: false, blocked: false, all: false };

// The view is a per-viewer convenience; storage can be missing or throw.
function storedView(): View {
  try {
    return localStorage.getItem("taskroll.view") === "list" ? "list" : "board";
  } catch {
    return "board";
  }
}

export function App() {
  const [meta, setMeta] = useState<Meta | null>(null);
  const [rows, setRows] = useState<Row[] | null>(null);
  const [filters, setFilters] = useState<Filters>(EMPTY);
  const [view, setView] = useState<View>(storedView);
  const [version, setVersion] = useState(0);
  const [creating, setCreating] = useState(false);
  const [toast, setToast] = useState<{ msg: string; error: boolean } | null>(null);
  const [itemId, openItem] = useItemRoute();
  const toastTimer = useRef<number | undefined>(undefined);

  const say = useCallback((msg: string, error = false) => {
    setToast({ msg, error });
    window.clearTimeout(toastTimer.current);
    toastTimer.current = window.setTimeout(() => setToast(null), error ? 8000 : 3000);
  }, []);

  const reload = useCallback(() => setVersion((v) => v + 1), []);

  useEffect(() => {
    api.meta().then(setMeta).catch((e: Error) => say(e.message, true));
  }, [version, say]);

  useEffect(() => {
    let live = true;
    const t = window.setTimeout(
      () => {
        api
          // The board always has a Done column, so it always asks for done
          // work and trims it to the recent window itself.
          .items(view === "board" ? { ...filters, all: true } : filters)
          .then((r) => live && setRows(r))
          .catch((e: Error) => live && say(e.message, true));
      },
      filters.text ? 200 : 0,
    );
    return () => {
      live = false;
      window.clearTimeout(t);
    };
  }, [filters, version, view, say]);

  // The CLI or another agent may change the records at any time.
  useEffect(() => {
    const t = window.setInterval(() => {
      if (!document.hidden && !creating) reload();
    }, 10000);
    return () => window.clearInterval(t);
  }, [creating, reload]);

  const changeView = (v: string) => {
    const next: View = v === "list" ? "list" : "board";
    setView(next);
    try {
      localStorage.setItem("taskroll.view", next);
    } catch {
      /* storage unavailable: the choice lasts this visit only */
    }
  };

  const boardRows =
    rows && view === "board" ? rows.filter((r) => r.status !== "dropped" && (filters.all || r.status !== "done" || recentlyDone(r.done, r.closed_at))) : rows;
  const hiddenDone =
    rows && view === "board" && !filters.all ? rows.filter((r) => r.status === "done" && !recentlyDone(r.done, r.closed_at)).length : 0;

  const move = async (id: string, status: Status) => {
    try {
      await api.setStatus(id, status);
      say(`${id} is ${STATUS_NAMES[status].toLowerCase()}`);
    } catch (e) {
      say((e as Error).message, true);
    }
    reload();
  };

  return (
    <div className="flex h-dvh flex-col bg-background text-foreground">
      <header className="flex flex-wrap items-center gap-3 border-b px-4 py-3 sm:px-6">
        <div className="flex items-baseline gap-2">
          <span className="text-base font-semibold tracking-tight">taskroll</span>
          {meta && <span className="hidden text-xs text-muted-foreground sm:inline">as {meta.actor}</span>}
        </div>
        <Tabs value={view} onValueChange={changeView}>
          <TabsList>
            <TabsTrigger value="board">
              <LayoutGrid /> Board
            </TabsTrigger>
            <TabsTrigger value="list">
              <List /> List
            </TabsTrigger>
          </TabsList>
        </Tabs>
        <div className="relative order-last w-full sm:order-none sm:ml-auto sm:w-72">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="search"
            aria-label="Search"
            placeholder="Search id, title, notes"
            className="pl-8"
            value={filters.text}
            onChange={(e) => setFilters({ ...filters, text: e.target.value })}
          />
        </div>
        <Button className="ml-auto sm:ml-0" onClick={() => setCreating(true)} disabled={!meta}>
          <Plus data-icon="inline-start" /> New task
        </Button>
      </header>

      <main className="flex min-h-0 flex-1 flex-col gap-4 px-4 py-4 sm:px-6">
        {meta && (
          <div className="flex flex-wrap items-center justify-between gap-2">
            <FilterBar meta={meta} filters={filters} onChange={setFilters} />
            {boardRows && <span className="text-xs text-muted-foreground tabular-nums">{boardRows.length} item(s)</span>}
          </div>
        )}
        {!rows ? (
          <div className="grid flex-1 grid-cols-1 gap-4 sm:grid-cols-3">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-64 rounded-xl" />
            ))}
          </div>
        ) : view === "board" ? (
          <Board
            rows={boardRows ?? []}
            showDone={filters.all}
            hiddenDone={hiddenDone}
            onShowAllDone={() => setFilters({ ...filters, all: true })}
            onOpen={openItem}
            onMove={move}
          />
        ) : (
          <ItemTable rows={rows} onOpen={openItem} />
        )}
      </main>

      {meta && (
        <NewTaskDialog
          open={creating}
          meta={meta}
          defaultEpic={filters.epic}
          onOpenChange={setCreating}
          onCreated={(row) => {
            setCreating(false);
            say(`Added ${row.id}`);
            reload();
            openItem(row.id);
          }}
        />
      )}

      <ItemSheet
        id={itemId}
        statuses={meta?.statuses ?? []}
        fieldLabels={Object.fromEntries((meta?.fields ?? []).map((f) => [f.key, f.label]))}
        version={version}
        onOpen={openItem}
        onClose={() => openItem(null)}
        onChanged={(msg) => {
          say(msg);
          reload();
        }}
        onError={(msg) => say(msg, true)}
      />

      <div aria-live="polite" role="status" className="pointer-events-none fixed right-4 bottom-4 z-[60] flex justify-end">
        {toast && (
          <div
            className={cn(
              "pointer-events-auto max-w-sm rounded-lg border bg-popover px-4 py-2.5 text-sm text-popover-foreground shadow-lg",
              toast.error && "border-destructive/50 text-destructive",
            )}
          >
            {toast.msg}
          </div>
        )}
      </div>
    </div>
  );
}
