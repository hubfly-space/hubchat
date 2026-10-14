import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { cn } from "../lib/cn";

/** One renderer for author previews and public articles. Raw HTML is disabled;
 * the default URL transform rejects executable URL schemes. */
export function Markdown({ children, className }: { children: string; className?: string }) {
  return <div className={cn("prose-hubchat break-words", className)}>
    <ReactMarkdown skipHtml remarkPlugins={[remarkGfm]} components={{
      a: ({ children, href, title }) => <a href={href} title={title} rel="noopener noreferrer">{children}</a>,
      img: ({ src, title, alt }) => <img src={src} title={title} loading="lazy" alt={alt ?? ""} />,
      table: ({ children }) => <div className="overflow-x-auto"><table>{children}</table></div>,
    }}>{children}</ReactMarkdown>
  </div>;
}
