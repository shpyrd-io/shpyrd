import type { CSSProperties, ReactNode } from "react";

// What an email is made of. A mail client reads no stylesheet it can rely
// on, no custom property and no flexbox: every style is written on its
// element, and what is laid out is a table. The colours are the library's
// tokens (design/ui/src/styles/index.css), turned from oklch into hex.
export const ink = {
  background: "#f5f5f5", // muted
  card: "#ffffff",
  foreground: "#0a0a0a",
  text: "#404040",
  muted: "#737373", // muted-foreground
  border: "#e5e5e5",
  primary: "#fa4d00",
  primaryForeground: "#ffffff",
};

export const sans = "Geist,-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif";
const mono = "'Geist Mono',ui-monospace,SFMono-Regular,Menlo,Consolas,monospace";

// The whole email, on white with no box around it, as the sign-up page
// is drawn: the mark, the words, the footer. `preview` is the
// line a mail client shows beside the subject; it is not drawn.
export function Email({
  preview,
  logo,
  footer,
  children,
}: {
  preview: string;
  logo: string;
  footer: ReactNode;
  children: ReactNode;
}) {
  return (
    <div id="email" style={{ margin: 0, padding: 0, background: ink.card }}>
      <div style={{ display: "none", maxHeight: 0, overflow: "hidden", opacity: 0, fontSize: 1, lineHeight: "1px" }}>{preview}</div>
      <table role="presentation" width="100%" cellPadding={0} cellSpacing={0} style={{ background: ink.card, fontFamily: sans }}>
        <tbody>
          <tr>
            <td align="center" style={{ padding: "40px 16px" }}>
              <table role="presentation" width="100%" cellPadding={0} cellSpacing={0} style={{ maxWidth: 520 }}>
                <tbody>
                  <tr>
                    <td style={{ padding: "0 0 28px" }}>
                      <table role="presentation" cellPadding={0} cellSpacing={0}>
                        <tbody>
                          <tr>
                            <td style={{ verticalAlign: "middle" }}>
                              <img src={logo} width={28} height={28} alt="" style={{ display: "block", border: 0 }} />
                            </td>
                            <td style={{ verticalAlign: "middle", paddingLeft: 8, fontSize: 18, fontWeight: 600, letterSpacing: "-0.02em", color: ink.foreground }}>
                              shpyrd
                            </td>
                          </tr>
                        </tbody>
                      </table>
                    </td>
                  </tr>
                  <tr>
                    <td>{children}</td>
                  </tr>
                  <tr>
                    <td style={{ padding: "32px 0 0", fontSize: 12, lineHeight: "18px", color: ink.muted }}>{footer}</td>
                  </tr>
                </tbody>
              </table>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  );
}

export function Heading({ children }: { children: ReactNode }) {
  return <h1 style={{ margin: "0 0 16px", fontSize: 20, lineHeight: "28px", fontWeight: 600, letterSpacing: "-0.01em", color: ink.foreground }}>{children}</h1>;
}

export function Text({ children, style }: { children: ReactNode; style?: CSSProperties }) {
  return <p style={{ margin: "0 0 16px", fontSize: 15, lineHeight: "24px", color: ink.text, ...style }}>{children}</p>;
}

// Small print at the end of the card: how long a link works, what to do
// if it was not asked for.
export function Small({ children }: { children: ReactNode }) {
  return <p style={{ margin: "24px 0 0", paddingTop: 20, borderTop: `1px solid ${ink.border}`, fontSize: 13, lineHeight: "20px", color: ink.muted }}>{children}</p>;
}

export function Strong({ children }: { children: ReactNode }) {
  return <strong style={{ fontWeight: 600, color: ink.foreground }}>{children}</strong>;
}

// The one thing to do. A table, so Outlook draws the colour too.
export function Button({ href, children }: { href: string; children: ReactNode }) {
  return (
    <table role="presentation" cellPadding={0} cellSpacing={0} style={{ margin: "8px 0 8px" }}>
      <tbody>
        <tr>
          <td style={{ background: ink.primary, borderRadius: 8 }}>
            <a
              href={href}
              style={{ display: "inline-block", padding: "12px 20px", fontSize: 15, fontWeight: 600, lineHeight: "20px", color: ink.primaryForeground, textDecoration: "none", borderRadius: 8 }}
            >
              {children}
            </a>
          </td>
        </tr>
      </tbody>
    </table>
  );
}

// The link the button holds, for a client that draws no button.
export function Fallback({ href }: { href: string }) {
  return (
    <p style={{ margin: "16px 0 0", fontSize: 13, lineHeight: "20px", color: ink.muted }}>
      Or paste this link into your browser:
      <br />
      <a href={href} style={{ color: ink.text, wordBreak: "break-all" }}>
        {href}
      </a>
    </p>
  );
}

// A code to type somewhere else.
export function Code({ children }: { children: ReactNode }) {
  return (
    <p
      style={{
        margin: "8px 0 16px",
        padding: "16px 0",
        background: ink.background,
        borderRadius: 8,
        textAlign: "center",
        fontFamily: mono,
        fontSize: 32,
        lineHeight: "40px",
        fontWeight: 600,
        letterSpacing: "0.3em",
        color: ink.foreground,
      }}
    >
      {children}
    </p>
  );
}

// Facts set out in two columns: what it is, and its value.
export function Facts({ rows }: { rows: [string, ReactNode][] }) {
  return (
    <table role="presentation" width="100%" cellPadding={0} cellSpacing={0} style={{ margin: "0 0 16px", background: ink.background, borderRadius: 8 }}>
      <tbody>
        {rows.map(([label, value], i) => (
          <tr key={label}>
            <td style={{ padding: i === 0 ? "14px 16px 6px" : i === rows.length - 1 ? "6px 16px 14px" : "6px 16px", fontSize: 13, lineHeight: "20px", color: ink.muted, width: "35%", verticalAlign: "top" }}>{label}</td>
            <td style={{ padding: i === 0 ? "14px 16px 6px" : i === rows.length - 1 ? "6px 16px 14px" : "6px 16px", fontSize: 13, lineHeight: "20px", color: ink.foreground, fontFamily: mono, wordBreak: "break-all" }}>
              {value}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
