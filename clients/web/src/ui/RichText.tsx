// Renders a message body with WhatsApp-style formatting (T6.01): *bold*,
// _italic_, ~strike~, `mono`, ```code blocks```, and autolinked URLs. The
// tokenizer is the shared pure one in @wa/media-pipeline; this only maps tokens
// to elements. Newlines are preserved via white-space: pre-wrap on the wrapper.

import { tokenizeRich } from "@wa/media-pipeline";
import { Fragment, type ReactNode } from "react";

export function RichText({ text }: { text: string }): ReactNode {
  const tokens = tokenizeRich(text);
  return (
    <span style={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>
      {tokens.map((tok, i) => {
        switch (tok.t) {
          case "b":
            return <strong key={i}>{tok.v}</strong>;
          case "i":
            return <em key={i}>{tok.v}</em>;
          case "s":
            return <s key={i}>{tok.v}</s>;
          case "code":
            return <code key={i} className="rich-code">{tok.v}</code>;
          case "pre":
            return <pre key={i} className="rich-pre">{tok.v}</pre>;
          case "link":
            return (
              <a key={i} href={tok.v} target="_blank" rel="noopener noreferrer nofollow" className="rich-link">
                {tok.v}
              </a>
            );
          default:
            return <Fragment key={i}>{tok.v}</Fragment>;
        }
      })}
    </span>
  );
}
