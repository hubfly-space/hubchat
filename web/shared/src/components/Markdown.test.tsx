import { act } from "react";
import { createRoot } from "react-dom/client";
import { expect, it } from "vitest";
import { Markdown } from "./Markdown";

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
it("renders rich Markdown while excluding executable HTML and URLs", async () => {
  const host = document.createElement("div"); const root = createRoot(host);
  await act(async () => root.render(<Markdown>{"## Heading\n\n- First\n- Second\n\n```js\nconst answer = 42;\n```\n\n[Safe](https://example.com) [Bad](javascript:alert(1))\n\n<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>"}</Markdown>));
  expect(host.querySelector("h2")?.textContent).toBe("Heading");
  expect(host.querySelectorAll("li").length).toBe(2);
  expect(host.querySelector("pre code")?.textContent).toContain("const answer");
  expect(host.querySelector("a")?.getAttribute("href")).toBe("https://example.com");
  expect(host.querySelector('a[href^="javascript:"]')).toBeNull();
  expect(host.querySelector("script, [onerror]")).toBeNull();
  await act(async () => root.unmount());
});
