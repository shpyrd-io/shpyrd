import { Changelog, ChangelogEntry, ChangelogSummary } from "@shpyrd/ui/components/changelog";
import { ChangelogArchive } from "@shpyrd/ui/components/changelog-archive";
import { Section } from "../../section";

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

// Gallery-only examples, with a year boundary on the next batch.
const archiveSamples = [
  ["2026", "Aug 12", "More reliable backups", "Scheduled backups recover automatically from temporary interruptions."],
  ["2026", "Jul 8", "Deployment overview", "Follow your latest deployment in one place."],
  ["2025", "Dec 18", "Custom domains", "Connect a domain to your app."],
  ["2025", "Nov 7", "Build history", "Review previous builds and their logs."],
  ["2025", "Oct 15", "Environment variables", "Manage configuration separately from your code."],
  ["2025", "Sep 2", "Faster log search", "Find the messages you need with less waiting."],
  ["2025", "Aug 20", "Workspace invites", "Invite teammates to your workspace."],
  ["2025", "Jun 9", "Volume overview", "See persistent storage alongside your apps."],
  ["2025", "Mar 11", "Usage summaries", "Review resource usage at a glance."],
  ["2025", "Jan 6", "Deployment notifications", "Know when your app is ready."],
  ["2024", "Dec 10", "App activity", "Follow recent changes in your workspace."],
  ["2024", "Oct 3", "Welcome to the changelog", "Product updates now have a home."],
];
const archive = archiveSamples.map(([year, date, title, summary], index) => ({
  slug: `gallery-update-${index}`,
  year,
  content: (
    <ChangelogSummary title={<a href="#example-detail">{title}</a>} date={date}
      key={`gallery-update-${index}`} tag={index % 3 === 0 ? "fix" : index % 3 === 1 ? "feature" : "improvement"}>
      {summary}
    </ChangelogSummary>
  ),
}));

export default function Page() {
  return (
    <>
      <Section title="Latest three updates · full detail">
        <Changelog>{entries}</Changelog>
      </Section>
      <ChangelogArchive entries={archive} title="Earlier updates" loadMore="Load older updates" />
      <div id="example-detail" className="scroll-mt-20"><Section title="Linked detail · illustrative destination in this gallery">
        <Changelog>
          <ChangelogEntry title="Welcome" date="Jan 5, 2026">
            <p>The first entry, with nothing to tell it apart.</p>
          </ChangelogEntry>
        </Changelog>
      </Section></div>
    </>
  );
}
