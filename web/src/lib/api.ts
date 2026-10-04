// The client for the server in internal/web. Writes carry the session token
// in a header the server requires; the token reaches the page in a meta tag,
// never in inline script.

export type Status = "todo" | "in_progress" | "in_review" | "done" | "dropped";

export interface Comment {
  date: string;
  author?: string;
  body: string;
}

export interface Row {
  id: string;
  type: string;
  status: Status;
  title: string;
  size?: string;
  size_note?: string;
  blocker?: boolean;
  depends?: string[];
  closes?: string[];
  notes?: string[];
  fields?: Record<string, string>;
  labels?: string[];
  done?: string;
  created_at?: string;
  created_by?: string;
  closed_at?: string;
  closed_by?: string;
  description?: string;
  comments?: Comment[];
  epic: string;
  ready: boolean;
  blocked_by?: string[];
  conditions?: string[];
  dependents?: string[];
}

export interface EpicSummary {
  id: string;
  title: string;
  open: number;
  done: number;
  ready: number;
  blocked: number;
}

export interface Meta {
  actor: string;
  statuses: Status[];
  sizes: string[] | null;
  types: string[] | null;
  labels: string[] | null;
  epics: EpicSummary[] | null;
  fields: { key: string; label: string; pattern?: string }[] | null;
  markers: { label: string; marker: string }[] | null;
  series: Record<string, string[]>;
}

export interface NewTask {
  epic: string;
  series?: string;
  title: string;
  size?: string;
  depends?: string[];
  fields?: Record<string, string>;
  labels?: string[];
  bug?: boolean;
  blocker?: boolean;
}

export interface Filters {
  epic: string;
  type: string;
  label: string;
  size: string;
  text: string;
  ready: boolean;
  blocked: boolean;
  all: boolean;
}

const token = document.querySelector<HTMLMetaElement>('meta[name="taskroll-token"]')?.content ?? "";

async function call<T>(path: string, body?: unknown): Promise<T> {
  const init: RequestInit =
    body === undefined
      ? { credentials: "same-origin" }
      : {
          method: "POST",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json", "X-Taskroll-Token": token },
          body: JSON.stringify(body),
        };
  const res = await fetch(path, init);
  const data = await res.json().catch(() => ({ error: res.statusText }));
  if (!res.ok) throw new Error((data as { error?: string }).error || res.statusText);
  return data as T;
}

export const api = {
  meta: () => call<Meta>("/api/meta"),
  items: (f: Filters) => {
    const q = new URLSearchParams();
    for (const k of ["epic", "type", "label", "size"] as const) if (f[k]) q.set(k, f[k]);
    if (f.text.trim()) q.set("text", f.text.trim());
    if (f.ready) q.set("ready", "1");
    if (f.blocked) q.set("blocked", "1");
    if (f.all) q.set("all", "1");
    return call<Row[]>("/api/items?" + q.toString());
  },
  item: (id: string) => call<Row>("/api/items/" + encodeURIComponent(id)),
  setStatus: (id: string, status: Status, on?: string) =>
    call<Row>("/api/items/" + encodeURIComponent(id) + "/status", on ? { status, on } : { status }),
  comment: (id: string, body: string) => call<Row>("/api/items/" + encodeURIComponent(id) + "/comments", { body }),
  addTask: (t: NewTask) => call<Row>("/api/tasks", t),
};
