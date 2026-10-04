import type { Status } from "./api";

export const STATUS_NAMES: Record<Status, string> = {
  todo: "To do",
  in_progress: "In progress",
  in_review: "In review",
  done: "Done",
  dropped: "Dropped",
};

export const BOARD_STATUSES: Status[] = ["todo", "in_progress", "in_review", "done"];

// A dot colour per status, shared by the board headers, badges and menus.
export const STATUS_DOT: Record<Status, string> = {
  todo: "bg-muted-foreground/50",
  in_progress: "bg-sky-500",
  in_review: "bg-amber-500",
  done: "bg-emerald-500",
  dropped: "bg-muted-foreground/30",
};

export function shortEpic(id: string): string {
  return /^epic-\d+/.exec(id)?.[0] ?? id;
}
