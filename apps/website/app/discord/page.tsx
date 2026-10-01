import { discord } from "@shpyrd/content/site/offer";

// /discord forwards to the project's Discord invite. The site is static files,
// so there is no server to answer with a redirect: the page asks the browser
// to go on at once (no script needed), and says where, for one that does not.
export const metadata = {
  title: "Discord",
  robots: { index: false },
};

export default function Page() {
  return (
    <main className="grid min-h-[50svh] place-items-center p-6 text-center">
      <meta httpEquiv="refresh" content={`0; url=${discord.invite}`} />
      <p className="text-muted-foreground">
        Taking you to the shpyrd Discord…{" "}
        <a href={discord.invite} className="text-foreground underline underline-offset-4">
          Go there now
        </a>
        .
      </p>
    </main>
  );
}
