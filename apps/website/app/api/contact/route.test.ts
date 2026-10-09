import { beforeEach, describe, expect, it, vi } from "vitest";

const send = vi.fn();
let configured = true;
vi.mock("@/lib/mailer", () => ({ mailer: () => (configured ? send : null) }));

const { POST } = await import("./route");
const { trap } = await import("@/lib/contact");

const answers = { firstName: "Ana", lastName: "Souza", email: "ana@acme.com", message: "We have six internal apps to move." };
const post = (body: unknown) =>
  POST(new Request("https://shpyrd.io/api/contact", {
    method: "POST",
    headers: { "content-type": "application/json", referer: "https://shpyrd.io/contact/sales", "x-vercel-ip-country": "BR" },
    body: typeof body === "string" ? body : JSON.stringify(body),
  }));
const human = { kind: "sales", answers, [trap]: "", elapsedMs: 10_000 };

describe("POST /api/contact", () => {
  beforeEach(() => {
    send.mockReset();
    configured = true;
  });

  it("sends a good form to the team, answered by the person", async () => {
    const res = await post(human);
    expect(res.status).toBe(200);
    expect(send).toHaveBeenCalledOnce();
    expect(send.mock.calls[0][0]).toMatchObject({ replyTo: "ana@acme.com", subject: "[Sales] Ana Souza, acme.com" });
    expect(send.mock.calls[0][0].text).toContain("Country: BR");
  });

  it("refuses a form with mistakes, field by field, and sends nothing", async () => {
    const res = await post({ ...human, answers: { ...answers, email: "nope" } });
    expect(res.status).toBe(400);
    expect((await res.json()).errors.email).toBeTruthy();
    expect(send).not.toHaveBeenCalled();
  });

  it("refuses what is not JSON, or no form it knows", async () => {
    expect((await post("not json")).status).toBe(400);
    expect((await post({ ...human, kind: "support" })).status).toBe(400);
    expect(send).not.toHaveBeenCalled();
  });

  it("answers a bot as if it had sent, and sends nothing", async () => {
    expect((await post({ ...human, [trap]: "https://spam.example" })).status).toBe(200);
    expect((await post({ ...human, elapsedMs: 0 })).status).toBe(200);
    expect(send).not.toHaveBeenCalled();
  });

  it("says it cannot send where it is not configured", async () => {
    configured = false;
    expect((await post(human)).status).toBe(503);
  });

  it("says the mail did not go when the server refuses it, and logs why but not what", async () => {
    const log = vi.spyOn(console, "error").mockImplementation(() => {});
    send.mockRejectedValueOnce(new Error("535 Authentication failed"));
    const res = await post(human);
    expect(res.status).toBe(502);
    expect(log).toHaveBeenCalledWith(expect.stringContaining("535 Authentication failed"));
    expect(JSON.stringify(log.mock.calls)).not.toContain("ana@acme.com");
    log.mockRestore();
  });
});
