import { useEffect, useState, type ReactNode } from "react";
import { ChevronDown, ExternalLink, Link2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import { api, type Row, type Status } from "@/lib/api";
import { STATUS_DOT, STATUS_NAMES } from "@/lib/status";
import { ItemBadges } from "./ItemBadges";

interface Props {
  id: string | null;
  statuses: Status[];
  fieldLabels: Record<string, string>;
  version: number; // bumps after any write or poll, so an open item reloads
  onOpen: (id: string) => void;
  onClose: () => void;
  onChanged: (msg: string) => void;
  onError: (msg: string) => void;
}

function IdLinks({ ids, onOpen }: { ids: string[]; onOpen: (id: string) => void }) {
  return (
    <span className="flex flex-wrap gap-x-2">
      {ids.map((id) => (
        <button key={id} type="button" className="font-mono text-xs text-primary underline-offset-4 hover:underline" onClick={() => onOpen(id)}>
          {id}
        </button>
      ))}
    </span>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 break-words">{children}</dd>
    </>
  );
}

function stamp(at?: string, by?: string) {
  if (!at && !by) return null;
  const when = at ? new Date(at).toLocaleString() : "";
  return [when, by && "by " + by].filter(Boolean).join(" ");
}

export function ItemSheet({ id, statuses, fieldLabels, version, onOpen, onClose, onChanged, onError }: Props) {
  const [row, setRow] = useState<Row | null>(null);
  const [missing, setMissing] = useState<string | null>(null);
  const [comment, setComment] = useState("");
  const [shipped, setShipped] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!id) {
      setRow(null);
      setMissing(null);
      return;
    }
    let live = true;
    api
      .item(id)
      .then((r) => live && (setRow(r), setMissing(null)))
      .catch((e: Error) => live && (setRow(null), setMissing(e.message)));
    return () => {
      live = false;
    };
  }, [id, version]);

  useEffect(() => {
    setComment("");
    setShipped("");
  }, [id]);

  const move = async (status: Status) => {
    if (!row) return;
    setBusy(true);
    try {
      const r = await api.setStatus(row.id, status, status === "done" ? shipped || undefined : undefined);
      setRow(r);
      onChanged(`${r.id} is ${STATUS_NAMES[status].toLowerCase()}`);
    } catch (e) {
      onError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const addComment = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!row || !comment.trim()) return;
    setBusy(true);
    try {
      const r = await api.comment(row.id, comment.trim());
      setRow(r);
      setComment("");
      onChanged(`Comment added to ${r.id}`);
    } catch (err) {
      onError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  // The repository link is the one to put in a document: it outlives this
  // session. Without browse_url, the local address is all there is.
  const copyLink = async () => {
    if (!row) return;
    try {
      await navigator.clipboard.writeText(row.url ?? window.location.origin + "/items/" + encodeURIComponent(row.id));
      onChanged(row.url ? "Link copied" : "Local link copied; set browse_url for one that works in docs");
    } catch {
      onError("Could not copy the link");
    }
  };

  const open = row?.status !== "done" && row?.status !== "dropped";

  return (
    <Sheet open={id !== null} onOpenChange={(o) => !o && onClose()}>
      <SheetContent className="w-full gap-0 overflow-y-auto data-[side=right]:sm:max-w-xl">
        {missing && (
          <SheetHeader>
            <SheetTitle>{id}</SheetTitle>
            <SheetDescription>{missing}</SheetDescription>
          </SheetHeader>
        )}
        {row && (
          <>
            <SheetHeader className="gap-2 pr-10">
              <div className="flex items-center gap-2 font-mono text-xs text-muted-foreground">
                {row.id}
                <Button variant="ghost" size="icon-xs" onClick={copyLink} aria-label="Copy link to this item" title="Copy link">
                  <Link2 />
                </Button>
                {row.url && (
                  <Button variant="ghost" size="icon-xs" asChild>
                    <a href={row.url} target="_blank" rel="noopener noreferrer" aria-label="Open in the repository" title="Open in the repository">
                      <ExternalLink />
                    </a>
                  </Button>
                )}
              </div>
              <SheetTitle className="text-lg leading-snug">{row.title}</SheetTitle>
              <SheetDescription className="sr-only">Details, status and comments for {row.id}</SheetDescription>
              <ItemBadges row={row} />
            </SheetHeader>

            <div className="flex flex-col gap-5 px-4 pb-6">
              <div className="flex flex-wrap items-end gap-3">
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button variant="outline" disabled={busy}>
                      <span className={cn("size-2 rounded-full", STATUS_DOT[row.status])} />
                      {STATUS_NAMES[row.status]}
                      <ChevronDown data-icon="inline-end" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="start">
                    {statuses
                      .filter((s) => s !== row.status)
                      .map((s) => (
                        <DropdownMenuItem key={s} onSelect={() => move(s)}>
                          <span className={cn("size-2 rounded-full", STATUS_DOT[s])} />
                          {STATUS_NAMES[s]}
                        </DropdownMenuItem>
                      ))}
                  </DropdownMenuContent>
                </DropdownMenu>
                {open && (
                  <div className="flex flex-col gap-1">
                    <Label htmlFor="shipped" className="text-xs text-muted-foreground">
                      Shipped on (for Done; empty is today)
                    </Label>
                    <Input id="shipped" type="date" className="w-44" value={shipped} onChange={(e) => setShipped(e.target.value)} />
                  </div>
                )}
              </div>

              <dl className="grid grid-cols-[8rem_1fr] gap-x-4 gap-y-2 text-sm">
                <Field label="Type">{row.type}</Field>
                {row.epic && <Field label="Epic">{row.epic}</Field>}
                {row.size && <Field label="Size">{row.size + (row.size_note ?? "")}</Field>}
                {row.done && <Field label="Done">{row.done}</Field>}
                {Object.entries(row.fields ?? {}).map(([k, v]) => (
                  <Field key={k} label={fieldLabels[k] ?? k}>
                    {v}
                  </Field>
                ))}
                {(row.depends?.length ?? 0) > 0 && (
                  <Field label="Depends on">
                    <IdLinks ids={row.depends!} onOpen={onOpen} />
                  </Field>
                )}
                {(row.blocked_by?.length ?? 0) > 0 && (
                  <Field label="Blocked by">
                    <IdLinks ids={row.blocked_by!} onOpen={onOpen} />
                  </Field>
                )}
                {(row.conditions?.length ?? 0) > 0 && <Field label="Waiting on">{row.conditions!.join("; ")}</Field>}
                {(row.dependents?.length ?? 0) > 0 && (
                  <Field label="Unblocks">
                    <IdLinks ids={row.dependents!} onOpen={onOpen} />
                  </Field>
                )}
                {(row.closes?.length ?? 0) > 0 && (
                  <Field label="Closes">
                    <IdLinks ids={row.closes!} onOpen={onOpen} />
                  </Field>
                )}
                {(row.notes ?? []).map((n, i) => (
                  <Field key={i} label="Note">
                    {n}
                  </Field>
                ))}
                {stamp(row.created_at, row.created_by) && <Field label="Created">{stamp(row.created_at, row.created_by)}</Field>}
                {stamp(row.closed_at, row.closed_by) && <Field label="Closed">{stamp(row.closed_at, row.closed_by)}</Field>}
              </dl>

              {row.description && (
                <>
                  <Separator />
                  <p className="whitespace-pre-wrap text-sm leading-relaxed">{row.description}</p>
                </>
              )}

              <Separator />
              <section aria-label="Comments" className="flex flex-col gap-3">
                <h3 className="text-sm font-medium">
                  Comments <span className="text-muted-foreground">{row.comments?.length ?? 0}</span>
                </h3>
                <ol className="flex flex-col gap-3">
                  {(row.comments ?? []).map((c, i) => (
                    <li key={i} className="rounded-lg border bg-muted/30 p-3">
                      <div className="mb-1 text-xs text-muted-foreground">
                        {c.author ?? "unknown"} · {c.date}
                      </div>
                      <p className="whitespace-pre-wrap text-sm">{c.body}</p>
                    </li>
                  ))}
                </ol>
                <form onSubmit={addComment} className="flex flex-col gap-2">
                  <Label htmlFor="comment" className="sr-only">
                    Add a comment
                  </Label>
                  <Textarea id="comment" rows={3} placeholder="Add a comment (markdown)" value={comment} onChange={(e) => setComment(e.target.value)} />
                  <Button type="submit" className="self-end" disabled={busy || !comment.trim()}>
                    Comment
                  </Button>
                </form>
              </section>
            </div>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}
