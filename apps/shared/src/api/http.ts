import { ApiError } from "./error";

// The calls to a shpyrd server, as the dashboard makes them: JSON in and
// out, the session in a cookie, and the CSRF cookie echoed as a header to
// prove intent. In development the address is proxied by Next; built,
// the application is served by the same server it calls.

function readCookie(name: string): string {
  const m = document.cookie.match(new RegExp("(?:^|; )" + name + "=([^;]*)"));
  return m ? decodeURIComponent(m[1]) : "";
}

function headers(init?: HeadersInit): Headers {
  const h = new Headers(init);
  const csrf = readCookie("shpyrd_csrf");
  if (csrf) h.set("X-Shpyrd-CSRF", csrf);
  return h;
}

async function handle(res: Response): Promise<Response> {
  if (res.ok) return res;
  let message = `${res.status} ${res.statusText}`;
  let code: string | undefined;
  try {
    const body = await res.json();
    if (body?.error) message = body.error;
    if (typeof body?.code === "string") code = body.code;
  } catch {
    // not json
  }
  throw new ApiError(res.status, message, code);
}

// The answer and the headers it came with, for a call whose headers say
// something the body does not.
export async function requestWithHeaders<T>(path: string, init: RequestInit = {}): Promise<{ data: T; headers: Headers }> {
  const res = await handle(await fetch(path, { ...init, headers: headers(init.headers) }));
  if (res.status === 204) return { data: undefined as T, headers: res.headers };
  const text = await res.text();
  return { data: (text ? JSON.parse(text) : undefined) as T, headers: res.headers };
}

export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  return (await requestWithHeaders<T>(path, init)).data;
}

// A text answer, like the output of a build: every line of it.
export async function text(path: string, init: RequestInit = {}): Promise<string> {
  const res = await handle(await fetch(path, { ...init, headers: headers(init.headers) }));
  return res.text();
}

// An answer that keeps coming: logs, the output of a build. Each line
// is given as it arrives, until the server ends it or `signal` aborts.
export async function stream(path: string, signal: AbortSignal, onLine: (line: string) => void): Promise<void> {
  const res = await handle(await fetch(path, { headers: headers(), signal }));
  const reader = res.body?.getReader();
  if (!reader) return;
  const decoder = new TextDecoder();
  let kept = "";
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    kept += decoder.decode(value, { stream: true });
    const lines = kept.split("\n");
    kept = lines.pop() ?? "";
    for (const line of lines) if (line !== "") onLine(line);
  }
  if (kept !== "") onLine(kept);
}

export function json(method: string, body: unknown): RequestInit {
  return { method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) };
}
