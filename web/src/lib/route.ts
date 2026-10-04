import { useCallback, useEffect, useState } from "react";

// The one route the app has beyond its root: /items/<ID>, so an item can be
// linked to and reopened from the address bar or the history.
const ITEM = /^\/items\/([^/]+)\/?$/;

function current(): string | null {
  const m = ITEM.exec(window.location.pathname);
  return m ? decodeURIComponent(m[1]) : null;
}

export function useItemRoute(): [string | null, (id: string | null) => void] {
  const [id, setId] = useState<string | null>(current);
  useEffect(() => {
    const onPop = () => setId(current());
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);
  const go = useCallback((next: string | null) => {
    const path = next ? "/items/" + encodeURIComponent(next) : "/";
    if (path !== window.location.pathname) window.history.pushState(null, "", path + window.location.search);
    setId(next);
  }, []);
  return [id, go];
}
