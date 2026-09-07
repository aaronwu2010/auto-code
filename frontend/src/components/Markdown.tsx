import React from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

/**
 * Markdown renderer for assistant messages.
 *
 * Supports full CommonMark + GFM (GitHub Flavored Markdown):
 *   - **bold**, *italic*, ~~strikethrough~~
 *   - `inline code` and ```fenced code blocks```
 *   - [links](url), ![images](url)
 *   - Tables, task lists, ordered/unordered lists
 *   - Blockquotes, headings (# ## ###)
 *
 * Code blocks get language-aware formatting via className="language-{lang}"
 * (syntax highlighting can be added later via rehype-highlight).
 */
interface MarkdownProps {
  content: string;
  className?: string;
}

export const Markdown: React.FC<MarkdownProps> = ({ content, className = "" }) => {
  return (
    <div
      className={`markdown-body text-sm leading-relaxed text-slate-200 break-words ${className}`}
    >
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          // --- Block elements ---
          p: ({ children }) => <p className="mb-2 last:mb-0">{children}</p>,

          h1: ({ children }) => (
            <h1 className="text-xl font-bold text-slate-100 mt-4 mb-2">{children}</h1>
          ),
          h2: ({ children }) => (
            <h2 className="text-lg font-bold text-slate-100 mt-3 mb-2">{children}</h2>
          ),
          h3: ({ children }) => (
            <h3 className="text-base font-semibold text-slate-100 mt-3 mb-1.5">
              {children}
            </h3>
          ),
          h4: ({ children }) => (
            <h4 className="text-sm font-semibold text-slate-100 mt-2 mb-1">{children}</h4>
          ),

          ul: ({ children }) => (
            <ul className="list-disc list-inside mb-2 space-y-0.5">{children}</ul>
          ),
          ol: ({ children }) => (
            <ol className="list-decimal list-inside mb-2 space-y-0.5">{children}</ol>
          ),
          li: ({ children }) => (
            <li className="pl-1">{children}</li>
          ),

          blockquote: ({ children }) => (
            <blockquote className="border-l-4 border-slate-600 pl-3 my-2 text-slate-400 italic">
              {children}
            </blockquote>
          ),

          hr: () => <hr className="border-slate-700 my-3" />,

          // --- Inline elements ---
          strong: ({ children }) => (
            <strong className="font-bold text-slate-100">{children}</strong>
          ),
          em: ({ children }) => <em className="italic">{children}</em>,
          del: ({ children }) => (
            <del className="line-through text-slate-500">{children}</del>
          ),

          a: ({ href, children }) => (
            <a
              href={href}
              target="_blank"
              rel="noopener noreferrer"
              className="text-cyan-400 hover:text-cyan-300 underline"
            >
              {children}
            </a>
          ),

          // --- Code ---
          code: ({ className, children, ...props }) => {
            // Fenced code blocks have className="language-{lang}"
            const isBlock = /language-/.test(className || "");
            if (isBlock) {
              return (
                <code
                  className={`${className} block bg-slate-950/80 border border-slate-700/50 rounded p-2 my-1 text-xs overflow-x-auto font-mono`}
                  {...props}
                >
                  {children}
                </code>
              );
            }
            // Inline code
            return (
              <code
                className="bg-slate-800 text-pink-300 px-1.5 py-0.5 rounded text-xs font-mono"
                {...props}
              >
                {children}
              </code>
            );
          },

          pre: ({ children }) => (
            <pre className="bg-slate-950/80 border border-slate-700/50 rounded-lg p-3 my-2 overflow-x-auto text-xs font-mono leading-relaxed">
              {children}
            </pre>
          ),

          // --- Tables ---
          table: ({ children }) => (
            <div className="overflow-x-auto my-2">
              <table className="w-full border-collapse text-xs">{children}</table>
            </div>
          ),
          thead: ({ children }) => (
            <thead className="bg-slate-800/50">{children}</thead>
          ),
          th: ({ children }) => (
            <th className="border border-slate-700 px-2 py-1 text-left font-semibold text-slate-100">
              {children}
            </th>
          ),
          td: ({ children }) => (
            <td className="border border-slate-700 px-2 py-1">{children}</td>
          ),

          // --- Images ---
          img: ({ src, alt }) => (
            <img
              src={src}
              alt={alt}
              className="max-w-full rounded my-2"
              loading="lazy"
            />
          ),
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
};

export default Markdown;
