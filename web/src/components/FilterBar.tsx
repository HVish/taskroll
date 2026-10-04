import { Checkbox } from "@/components/ui/checkbox";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import type { Filters, Meta } from "@/lib/api";

const ALL = "__all";

function FilterSelect({ label, value, options, onChange }: { label: string; value: string; options: { value: string; text: string }[]; onChange: (v: string) => void }) {
  return (
    <Select value={value || ALL} onValueChange={(v) => onChange(v === ALL ? "" : v)}>
      <SelectTrigger size="sm" aria-label={label} className="min-w-32">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={ALL}>{label}</SelectItem>
        {options.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.text}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

export function FilterBar({ meta, filters, onChange }: { meta: Meta; filters: Filters; onChange: (f: Filters) => void }) {
  const set = <K extends keyof Filters>(k: K, v: Filters[K]) => onChange({ ...filters, [k]: v });
  const list = (xs: string[] | null) => (xs ?? []).map((x) => ({ value: x, text: x }));
  return (
    <div className="flex flex-wrap items-center gap-2">
      <FilterSelect label="All epics" value={filters.epic} options={(meta.epics ?? []).map((e) => ({ value: e.id, text: e.title }))} onChange={(v) => set("epic", v)} />
      <FilterSelect label="Any type" value={filters.type} options={list(meta.types)} onChange={(v) => set("type", v)} />
      <FilterSelect label="Any label" value={filters.label} options={list(meta.labels)} onChange={(v) => set("label", v)} />
      <FilterSelect label="Any size" value={filters.size} options={list(meta.sizes)} onChange={(v) => set("size", v)} />
      <div className="ml-1 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
        <label htmlFor="f-ready" className="flex items-center gap-2">
          <Checkbox id="f-ready" checked={filters.ready} onCheckedChange={(v) => set("ready", v === true)} /> Ready
        </label>
        <label htmlFor="f-blocked" className="flex items-center gap-2">
          <Checkbox id="f-blocked" checked={filters.blocked} onCheckedChange={(v) => set("blocked", v === true)} /> Blocked
        </label>
        <label htmlFor="f-show-done" className="flex items-center gap-2">
          <Checkbox id="f-show-done" checked={filters.all} onCheckedChange={(v) => set("all", v === true)} /> Show all done
        </label>
      </div>
    </div>
  );
}
