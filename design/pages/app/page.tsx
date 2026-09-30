import Link from "next/link";

// The pages, for whoever opens the development server; the server never
// serves this one.
export default function Page() {
  return (
    <ul className="grid gap-2 p-6 text-sm">
      {["nothing", "no-access", "waking", "mark"].map((p) => (
        <li key={p}>
          <Link href={`/${p}`} className="underline">
            {p}
          </Link>
        </li>
      ))}
    </ul>
  );
}
