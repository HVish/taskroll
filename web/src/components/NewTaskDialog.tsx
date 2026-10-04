import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { api, type Meta, type Row } from "@/lib/api";

interface Props {
  open: boolean;
  meta: Meta;
  defaultEpic: string;
  onOpenChange: (open: boolean) => void;
  onCreated: (row: Row) => void;
}

const NONE = "__none";

export function NewTaskDialog({ open, meta, defaultEpic, onOpenChange, onCreated }: Props) {
  const epics = meta.epics ?? [];
  const [epic, setEpic] = useState("");
  const [series, setSeries] = useState("");
  const [title, setTitle] = useState("");
  const [size, setSize] = useState(NONE);
  const [depends, setDepends] = useState("");
  const [fields, setFields] = useState<Record<string, string>>({});
  const [markers, setMarkers] = useState<string[]>([]);
  const [blocker, setBlocker] = useState(false);
  const [bug, setBug] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const pickEpic = (id: string) => {
    setEpic(id);
    const s = meta.series[id] ?? [];
    setSeries(s.length === 1 ? s[0] : "");
  };

  useEffect(() => {
    if (!open) return;
    pickEpic(defaultEpic || epics[0]?.id || "");
    setTitle("");
    setSize(NONE);
    setDepends("");
    setFields({});
    setMarkers([]);
    setBlocker(false);
    setBug(false);
    setError("");
  }, [open]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const row = await api.addTask({
        epic,
        series: series.trim(),
        title: title.trim(),
        size: size === NONE ? undefined : size,
        depends: depends.split(",").map((s) => s.trim()).filter(Boolean),
        fields: Object.fromEntries(Object.entries(fields).filter(([, v]) => v.trim())),
        labels: markers,
        blocker,
        bug,
      });
      onCreated(row);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={submit} className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>New task</DialogTitle>
            <DialogDescription>Added to the epic's records and its generated markdown.</DialogDescription>
          </DialogHeader>
          {epics.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              There are no epics yet. Create one with <code className="font-mono">taskroll epic new</code>.
            </p>
          ) : (
            <>
              <div className="grid grid-cols-[1fr_8rem] gap-3">
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="t-epic">Epic</Label>
                  <Select value={epic} onValueChange={pickEpic}>
                    <SelectTrigger id="t-epic" className="w-full">
                      <SelectValue placeholder="Choose an epic" />
                    </SelectTrigger>
                    <SelectContent>
                      {epics.map((e) => (
                        <SelectItem key={e.id} value={e.id}>
                          {e.title} <span className="text-muted-foreground">({e.id})</span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="t-series">Series</Label>
                  <Input id="t-series" required pattern="[A-Za-z]+" autoComplete="off" placeholder="PAY" value={series} onChange={(e) => setSeries(e.target.value)} />
                </div>
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="t-title">Title</Label>
                <Input id="t-title" required autoFocus autoComplete="off" value={title} onChange={(e) => setTitle(e.target.value)} />
              </div>
              <div className="grid grid-cols-[8rem_1fr] gap-3">
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="t-size">Size</Label>
                  <Select value={size} onValueChange={setSize}>
                    <SelectTrigger id="t-size" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value={NONE}>None</SelectItem>
                      {(meta.sizes ?? []).map((s) => (
                        <SelectItem key={s} value={s}>
                          {s}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="t-depends">Depends on</Label>
                  <Input id="t-depends" autoComplete="off" placeholder="PAY-001, a human decision" value={depends} onChange={(e) => setDepends(e.target.value)} />
                </div>
              </div>
              {(meta.fields ?? []).map((f) => (
                <div key={f.key} className="flex flex-col gap-1.5">
                  <Label htmlFor={"t-f-" + f.key}>{f.label}</Label>
                  <Input
                    id={"t-f-" + f.key}
                    autoComplete="off"
                    pattern={f.pattern ? f.pattern.replace(/^\^|\$$/g, "") : undefined}
                    value={fields[f.key] ?? ""}
                    onChange={(e) => setFields({ ...fields, [f.key]: e.target.value })}
                  />
                </div>
              ))}
              <div className="flex flex-wrap gap-x-5 gap-y-2">
                <label htmlFor="t-bug" className="flex items-center gap-2 text-sm">
                  <Checkbox id="t-bug" checked={bug} onCheckedChange={(v) => setBug(v === true)} /> Bug
                </label>
                <label htmlFor="t-blocker" className="flex items-center gap-2 text-sm">
                  <Checkbox id="t-blocker" checked={blocker} onCheckedChange={(v) => setBlocker(v === true)} /> Blocker
                </label>
                {(meta.markers ?? []).map((m) => (
                  <label key={m.label} htmlFor={"t-m-" + m.label} className="flex items-center gap-2 text-sm">
                    <Checkbox
                      id={"t-m-" + m.label}
                      checked={markers.includes(m.label)}
                      onCheckedChange={(v) => setMarkers(v === true ? [...markers, m.label] : markers.filter((x) => x !== m.label))}
                    />
                    {m.marker}
                  </label>
                ))}
              </div>
              {error && (
                <p role="alert" className="text-sm text-destructive">
                  {error}
                </p>
              )}
            </>
          )}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy || epics.length === 0}>
              Add task
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
