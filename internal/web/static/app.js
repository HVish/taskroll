// The tracker's web UI. Every string from the records goes into the page as
// text (textContent), never as markup, so a title holding HTML shows as the
// characters it is. Writes send the session token in a header the server
// requires; the token comes from a meta tag, not inline script.
"use strict";

const token = document.querySelector('meta[name="taskroll-token"]').content;
const $ = (id) => document.getElementById(id);
const STATUS_NAMES = { todo: "To do", in_progress: "In progress", in_review: "In review", done: "Done", dropped: "Dropped" };
const BOARD = ["todo", "in_progress", "in_review", "done"];
let meta = null;
let view = "board";
let current = null; // the item open in the detail panel

function el(tag, props, ...children) {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(props || {})) {
    if (k === "text") node.textContent = v;
    else if (k === "class") node.className = v;
    else if (k.startsWith("on")) node.addEventListener(k.slice(2), v);
    else if (v !== undefined && v !== null && v !== false) node.setAttribute(k, v === true ? "" : v);
  }
  for (const c of children) if (c) node.append(c);
  return node;
}

async function api(path, body) {
  const opts = body === undefined ? {} : {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Taskroll-Token": token },
    body: JSON.stringify(body),
  };
  const res = await fetch(path, { credentials: "same-origin", ...opts });
  const data = await res.json().catch(() => ({ error: res.statusText }));
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
}

function say(msg, isError) {
  const line = $("status-line");
  line.textContent = msg || "";
  line.classList.toggle("error", !!isError);
}

function shortEpic(id) {
  const m = /^epic-\d+/.exec(id || "");
  return m ? m[0] : (id || "");
}

function fillSelect(select, values, label) {
  const keep = select.value;
  select.replaceChildren(el("option", { value: "", text: label }));
  for (const v of values) select.append(el("option", { value: v.value ?? v, text: v.text ?? v }));
  select.value = keep;
}

async function loadMeta() {
  meta = await api("/api/meta");
  $("who").textContent = "as " + meta.actor;
  fillSelect($("f-epic"), meta.epics.map((e) => ({ value: e.id, text: e.id })), "All epics");
  fillSelect($("f-type"), meta.types, "Any type");
  fillSelect($("f-label"), meta.labels, "Any label");
  fillSelect($("f-size"), meta.sizes, "Any size");
}

function query() {
  const q = new URLSearchParams();
  const add = (k, v) => { if (v) q.append(k, v); };
  add("epic", $("f-epic").value);
  add("type", $("f-type").value);
  add("label", $("f-label").value);
  add("size", $("f-size").value);
  add("text", $("f-text").value.trim());
  if ($("f-ready").checked) q.set("ready", "1");
  if ($("f-blocked").checked) q.set("blocked", "1");
  if ($("f-done").checked) q.set("all", "1");
  return q;
}

async function refresh() {
  try {
    const rows = await api("/api/items?" + query());
    if (view === "board") renderBoard(rows); else renderList(rows);
    say(rows.length + " item(s)");
    if (current) {
      const again = rows.find((r) => r.id === current.id);
      if (again) showDetail(again);
    }
  } catch (err) {
    say(err.message, true);
  }
}

function tags(row) {
  const t = el("div", { class: "tags" });
  if (row.size) t.append(el("span", { class: "tag", text: row.size }));
  if (row.epic) t.append(el("span", { class: "tag", text: shortEpic(row.epic) }));
  if (!row.epic) t.append(el("span", { class: "tag", text: row.type }));
  for (const l of row.labels || []) t.append(el("span", { class: "tag", text: l }));
  const blocked = [...(row.blocked_by || []), ...(row.conditions || [])];
  if (blocked.length) t.append(el("span", { class: "tag blocked", text: "blocked: " + blocked.join(", ") }));
  else if (row.ready) t.append(el("span", { class: "tag ready", text: "ready" }));
  return t;
}

function card(row) {
  const c = el("article", { class: "card", tabindex: "0", draggable: "true" },
    el("div", { class: "id", text: row.id + (row.type === "bug" ? " · bug" : "") + (row.blocker ? " · blocker" : "") }),
    el("div", { class: "title", text: row.title }),
    tags(row));
  c.addEventListener("click", () => showDetail(row));
  c.addEventListener("keydown", (e) => { if (e.key === "Enter") showDetail(row); });
  c.addEventListener("dragstart", (e) => { e.dataTransfer.setData("text/plain", row.id); });
  return c;
}

function renderBoard(rows) {
  const board = $("board");
  board.replaceChildren();
  const statuses = $("f-done").checked ? BOARD : BOARD.filter((s) => s !== "done");
  for (const status of statuses) {
    const items = rows.filter((r) => r.status === status);
    const col = el("div", { class: "column", "data-status": status },
      el("h2", {}, el("span", { text: STATUS_NAMES[status] }), el("span", { text: String(items.length) })));
    for (const row of items) col.append(card(row));
    col.addEventListener("dragover", (e) => { e.preventDefault(); col.classList.add("drop"); });
    col.addEventListener("dragleave", () => col.classList.remove("drop"));
    col.addEventListener("drop", (e) => {
      e.preventDefault();
      col.classList.remove("drop");
      const id = e.dataTransfer.getData("text/plain");
      if (id) setStatus(id, status);
    });
    board.append(col);
  }
}

function renderList(rows) {
  const body = $("list-body");
  body.replaceChildren();
  for (const row of rows) {
    const tr = el("tr", { tabindex: "0" },
      el("td", { text: row.id }),
      el("td", { text: STATUS_NAMES[row.status] || row.status }),
      el("td", { text: row.size || "-" }),
      el("td", { text: row.epic ? shortEpic(row.epic) : row.type }),
      el("td", { text: row.title }),
      el("td", { text: [...(row.blocked_by || []), ...(row.conditions || [])].join(", ") }));
    tr.addEventListener("click", () => showDetail(row));
    tr.addEventListener("keydown", (e) => { if (e.key === "Enter") showDetail(row); });
    body.append(tr);
  }
}

function showDetail(row) {
  current = row;
  $("detail").hidden = false;
  $("d-title").textContent = row.id + "  " + row.title;
  const dl = $("d-meta");
  dl.replaceChildren();
  const pair = (k, v) => { if (v) dl.append(el("dt", { text: k }), el("dd", { text: v })); };
  pair("Status", STATUS_NAMES[row.status] || row.status);
  pair("Type", row.type);
  pair("Epic", row.epic);
  pair("Size", row.size);
  pair("Done", row.done);
  pair("Labels", (row.labels || []).join(", "));
  for (const [k, v] of Object.entries(row.fields || {})) pair(k, v);
  pair("Depends", (row.depends || []).join(", "));
  pair("Blocked by", (row.blocked_by || []).join(", "));
  pair("Conditions", (row.conditions || []).join("; "));
  pair("Dependents", (row.dependents || []).join(", "));
  pair("Closes", (row.closes || []).join(", "));
  for (const n of row.notes || []) pair("Note", n);
  pair("Created", [row.created_at, row.created_by].filter(Boolean).join(" by "));
  pair("Closed", [row.closed_at, row.closed_by].filter(Boolean).join(" by "));

  const actions = $("d-actions");
  actions.replaceChildren();
  for (const status of meta.statuses) {
    if (status === row.status) continue;
    actions.append(el("button", { type: "button", text: "→ " + STATUS_NAMES[status], onclick: () => setStatus(row.id, status) }));
  }
  if (row.status !== "done") {
    actions.append(el("label", { title: "The date it shipped, for → Done; empty means today" }, "Shipped ", el("input", { type: "date", id: "d-on" })));
  }
  $("d-description").textContent = row.description || "";
  const list = $("d-comments");
  list.replaceChildren();
  for (const c of row.comments || []) {
    list.append(el("li", {}, el("div", { class: "by", text: c.date + " · " + (c.author || "-") }), el("div", { class: "body", text: c.body })));
  }
}

// setStatus moves an item. Done takes the ship date from the detail panel's
// date field when it is open on this item, and today otherwise.
async function setStatus(id, status) {
  const body = { status };
  const on = $("d-on");
  if (status === "done" && on && current && current.id === id && on.value) body.on = on.value;
  try {
    await api("/api/items/" + encodeURIComponent(id) + "/status", body);
    say(id + " is " + (STATUS_NAMES[status] || status));
    await refresh();
  } catch (err) {
    say(err.message, true);
  }
}

$("d-comment-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const text = $("d-comment").value.trim();
  if (!current || !text) return;
  try {
    const row = await api("/api/items/" + encodeURIComponent(current.id) + "/comments", { body: text });
    $("d-comment").value = "";
    showDetail(row);
    say("comment added to " + row.id);
  } catch (err) {
    say(err.message, true);
  }
});

$("d-close").addEventListener("click", () => { $("detail").hidden = true; current = null; });
document.addEventListener("keydown", (e) => { if (e.key === "Escape") { $("detail").hidden = true; current = null; } });

for (const tab of document.querySelectorAll("[data-view]")) {
  tab.addEventListener("click", () => {
    view = tab.dataset.view;
    for (const t of document.querySelectorAll("[data-view]")) t.setAttribute("aria-selected", String(t === tab));
    $("board").hidden = view !== "board";
    $("list").hidden = view !== "list";
    refresh();
  });
}
for (const id of ["f-epic", "f-type", "f-label", "f-size", "f-ready", "f-blocked", "f-done"]) {
  $(id).addEventListener("change", refresh);
}
let typing;
$("f-text").addEventListener("input", () => { clearTimeout(typing); typing = setTimeout(refresh, 250); });

// New task.
function openTaskDialog() {
  const epic = $("t-epic");
  epic.replaceChildren();
  for (const e of meta.epics) epic.append(el("option", { value: e.id, text: e.id + " - " + e.title }));
  if ($("f-epic").value) epic.value = $("f-epic").value;
  fillSelect($("t-size"), meta.sizes, "None");
  const fields = $("t-fields");
  fields.replaceChildren();
  for (const f of meta.fields) fields.append(el("label", {}, el("span", { text: f.label }), el("input", { "data-field": f.key, pattern: f.pattern ? f.pattern.replace(/^\^|\$$/g, "") : null, autocomplete: "off" })));
  for (const m of meta.markers) fields.append(el("label", { class: "check" }, el("input", { type: "checkbox", "data-marker": m.label }), el("span", { text: m.marker })));
  suggestSeries();
  $("t-error").textContent = "";
  $("task-dialog").showModal();
  $("t-title").focus();
}

function suggestSeries() {
  const series = meta.series[$("t-epic").value] || [];
  $("t-series").value = series.length === 1 ? series[0] : "";
}

$("t-epic").addEventListener("change", suggestSeries);
$("new-task").addEventListener("click", openTaskDialog);
$("t-cancel").addEventListener("click", () => $("task-dialog").close());
$("task-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const task = {
    epic: $("t-epic").value,
    series: $("t-series").value.trim(),
    title: $("t-title").value.trim(),
    size: $("t-size").value,
    depends: $("t-depends").value.split(",").map((s) => s.trim()).filter(Boolean),
    blocker: $("t-blocker").checked,
    fields: {},
    labels: [],
  };
  for (const input of document.querySelectorAll("[data-field]")) if (input.value.trim()) task.fields[input.dataset.field] = input.value.trim();
  for (const box of document.querySelectorAll("[data-marker]")) if (box.checked) task.labels.push(box.dataset.marker);
  try {
    const row = await api("/api/tasks", task);
    $("task-dialog").close();
    $("task-form").reset();
    say("added " + row.id);
    await loadMeta();
    await refresh();
    showDetail(row);
  } catch (err) {
    $("t-error").textContent = err.message;
  }
});

// Another agent or the CLI may change the records at any time.
setInterval(() => { if (!document.hidden && !$("task-dialog").open) refresh(); }, 10000);

loadMeta().then(refresh).catch((err) => say(err.message, true));
