"use client";

import { Copy, Rss } from "lucide-react";
import { Button } from "@shpyrd/ui/components/button";
import { Changelog, ChangelogEntry } from "@shpyrd/ui/components/changelog";
import { Section } from "../../section";

const stay = (event: React.MouseEvent) => event.preventDefault();

const entries = (
  <>
    <ChangelogEntry title="Rollbacks in one click" date="Oct 7, 2026" tag="feature">
      <p>Every deploy is a numbered release, and you can go back to any of them from its page.</p>
      <ul>
        <li>The previous release is kept warm for an hour.</li>
        <li>Config changes roll back with the code.</li>
      </ul>
    </ChangelogEntry>
    <ChangelogEntry title="Faster cold starts" date="Sep 29, 2026" tag="improvement">
      <p>An app that has been idle starts in about half the time it did. Nothing to change on your side.</p>
    </ChangelogEntry>
    <ChangelogEntry title="Logs no longer drop the last line" date="Sep 18, 2026" tag="fix">
      <p>
        The last line written before a process exited was sometimes lost. It now reaches <code>shpyrd logs</code>.
      </p>
    </ChangelogEntry>
  </>
);

export default function Page() {
  return (
    <>
      <Section title="With a head: title, a line and the actions at the right">
        <Changelog
          title="Changelog"
          description="What is new in shpyrd, newest first."
          actions={
            <>
              <Button variant="outline" size="sm" onClick={stay}>
                <Rss /> RSS
              </Button>
              <Button variant="outline" size="sm" onClick={stay}>
                <Copy /> Copy as Markdown
              </Button>
            </>
          }
        >
          {entries}
        </Changelog>
      </Section>

      <Section title="Without a head: only the rail">
        <Changelog>{entries}</Changelog>
      </Section>

      <Section title="An entry without a tag">
        <Changelog>
          <ChangelogEntry title="Welcome" date="Jan 5, 2026">
            <p>The first entry, with nothing to tell it apart.</p>
          </ChangelogEntry>
        </Changelog>
      </Section>
    </>
  );
}
