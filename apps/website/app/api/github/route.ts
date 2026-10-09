// The repository's stars, for the GitHub link in the header (shell.tsx):
// asked of GitHub at most once an hour, so a visit never waits on it and the
// site stays well under GitHub's limit for calls made without a token. When
// GitHub does not answer, no count, and the link shows its mark alone.
export const revalidate = 3600;

export async function GET() {
  try {
    const res = await fetch("https://api.github.com/repos/shpyrd-io/shpyrd", {
      headers: { accept: "application/vnd.github+json" },
      next: { revalidate },
    });
    const repo = res.ok ? await res.json() : null;
    return Response.json({ stars: typeof repo?.stargazers_count === "number" ? repo.stargazers_count : null });
  } catch {
    return Response.json({ stars: null });
  }
}
