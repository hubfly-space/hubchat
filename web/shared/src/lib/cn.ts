import { clsx, type ClassValue } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

/**
 * tailwind-merge has no knowledge of our custom scale names, so conflicting
 * classes like `text-md text-lg` or `shadow-2 shadow-3` would both survive.
 * Teaching it the custom groups keeps `cn()` honest at call sites.
 */
const twMerge = extendTailwindMerge({
  extend: {
    classGroups: {
      "font-size": [{ text: ["2xs", "xs", "sm", "base", "md", "lg", "xl", "2xl", "3xl", "4xl"] }],
      shadow: [{ shadow: ["1", "2", "3", "4", "none"] }],
      rounded: [{ rounded: ["xs", "sm", "md", "lg", "xl", "2xl", "full", "none"] }],
    },
  },
});

/** Compose class names with conflict resolution. The only way to merge classes. */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
