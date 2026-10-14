import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { clearQueryCache, invalidate, useInfinite, useQuery } from "./query";
import type { Paginated } from "../types";

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const deferred = <T,>() => { let resolve!: (value: T) => void; const promise = new Promise<T>((done) => { resolve = done; }); return { promise, resolve }; };
let host: HTMLDivElement;
let root: Root;
beforeEach(() => { localStorage.clear(); clearQueryCache(); host = document.createElement("div"); document.body.append(host); root = createRoot(host); });
afterEach(async () => { await act(async () => root.unmount()); clearQueryCache(); host.remove(); });

 describe("scoped server state", () => {
  it("never shows a previous workspace or accepts its late response", async () => {
    const first = deferred<string>(); let aborted = false;
    function Probe({ workspace }: { workspace: string }) {
      const query = useQuery(["companies"], (signal) => {
        signal.addEventListener("abort", () => { aborted = true; });
        return workspace === "alpha" ? first.promise : Promise.resolve("BETA ONLY");
      });
      return <p>{query.data ?? "loading"}</p>;
    }
    localStorage.setItem("hubchat.workspace", JSON.stringify("alpha"));
    await act(async () => root.render(<Probe workspace="alpha" />));
    localStorage.setItem("hubchat.workspace", JSON.stringify("beta"));
    await act(async () => root.render(<Probe workspace="beta" />));
    expect(host.textContent).toBe("BETA ONLY");
    await act(async () => first.resolve("ALPHA ONLY"));
    expect(aborted).toBe(true);
    expect(host.textContent).toBe("BETA ONLY");
  });

  it("refetches when an invalidation arrives during a fetch", async () => {
    const first = deferred<string>(); let calls = 0;
    function Probe() { const query = useQuery(["counts"], () => ++calls === 1 ? first.promise : Promise.resolve("fresh")); return <p>{query.data}</p>; }
    await act(async () => root.render(<Probe />));
    await act(async () => invalidate(["counts"]));
    await act(async () => first.resolve("stale"));
    expect(calls).toBe(2);
    expect(host.textContent).toBe("fresh");
  });

  it("drops an older pagination response after a filter change", async () => {
    const next = deferred<Paginated<{ id: string }>>();
    let loadMore!: () => Promise<void>;
    function Probe({ filter }: { filter: string }) {
      const query = useInfinite(["rows", filter], (cursor) => cursor ? next.promise : Promise.resolve({ data: [{ id: filter }], next_cursor: "next", has_more: true }));
      loadMore = query.fetchNext;
      return <p>{query.items.map((row) => row.id).join(",")}</p>;
    }
    await act(async () => root.render(<Probe filter="old" />));
    await act(async () => { void loadMore(); });
    await act(async () => root.render(<Probe filter="new" />));
    await act(async () => next.resolve({ data: [{ id: "old-page-2" }], next_cursor: null, has_more: false }));
    expect(host.textContent).toBe("new");
  });
 });
