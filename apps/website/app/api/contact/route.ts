// The contact forms' one route: checks a submission by the forms' rules
// (src/lib/contact.ts) and mails it to the team (src/lib/mailer.ts).
// The only part of the site that runs on request.
import { check, isBot, isKind, type Answers } from "@/lib/contact";
import { contactMail } from "@/lib/contact-mail";
import { mailer } from "@/lib/mailer";

export const runtime = "nodejs";

export async function POST(request: Request) {
  let body: { kind?: unknown; answers?: unknown; website?: unknown; startedAt?: unknown };
  try {
    body = await request.json();
  } catch {
    return Response.json({ error: "The form could not be read." }, { status: 400 });
  }
  if (!body || !isKind(body.kind)) {
    return Response.json({ error: "No such form." }, { status: 400 });
  }
  // A bot learns nothing: it is answered as a person would be.
  if (isBot(body, Date.now())) return Response.json({ ok: true });

  const answers = (body.answers && typeof body.answers === "object" ? body.answers : {}) as Answers;
  const errors = check(body.kind, answers);
  if (Object.keys(errors).length > 0) return Response.json({ errors }, { status: 400 });

  const send = mailer();
  if (!send) return Response.json({ error: "The form cannot be sent from here." }, { status: 503 });

  const mail = contactMail(body.kind, answers, {
    page: request.headers.get("referer") ?? "",
    at: new Date(),
    country: request.headers.get("x-vercel-ip-country") ?? undefined,
  });
  try {
    await send(mail);
  } catch (err) {
    // Why it failed, never what the person wrote.
    console.error(`contact: the ${body.kind} form was not mailed: ${err instanceof Error ? err.message : String(err)}`);
    return Response.json({ error: "The form could not be sent." }, { status: 502 });
  }
  return Response.json({ ok: true });
}
