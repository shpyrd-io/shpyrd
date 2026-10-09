"use client";

import { MediaPlaylist, type MediaItem } from "@shpyrd/ui/components/media-playlist";
import { Section } from "../../section";

const base = "https://interactive-examples.mdn.mozilla.net/media/cc0-videos";

const items: MediaItem[] = [
  {
    id: "first-app",
    title: "Ship your first app",
    src: `${base}/flower.mp4`,
    duration: "0:05",
    description: "From a folder of code to a link your team can open.",
  },
  {
    id: "share",
    title: "Share it with your team",
    src: `${base}/friday.mp4`,
    duration: "0:23",
    description: "Choose who may sign in to it.",
  },
  {
    id: "rollback",
    title: "Roll back a bad change",
    src: `${base}/flower.mp4`,
    duration: "0:05",
    description: "Go back to a release that worked, in one step.",
  },
  {
    id: "worker",
    title: "Run a worker",
    src: `${base}/friday.mp4`,
    duration: "0:23",
    description: "Work that goes on after the page has been answered.",
  },
  {
    id: "domain",
    title: "Use your own domain",
    src: `${base}/flower.mp4`,
    duration: "0:05",
  },
];

export default function Page() {
  return (
    <>
      <Section title="The player larger, the list beside it, which scrolls within the player's height">
        <MediaPlaylist items={items} />
      </Section>

      <Section title="Opened on another video, in a narrower place where the list goes under">
        <MediaPlaylist className="max-w-xl" items={items} initialId="rollback" />
      </Section>
    </>
  );
}
