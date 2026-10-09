"use client";

import { VideoPlayer } from "@shpyrd/ui/components/video-player";
import { Section } from "../../section";

const flower = "https://interactive-examples.mdn.mozilla.net/media/cc0-videos/flower.mp4";
const gradient = "linear-gradient(135deg, #ff4f00 0%, #7a1d00 55%, #120400 100%)";

export default function Page() {
  return (
    <>
      <Section title="A gradient for a poster, and the large button over it until it plays">
        <VideoPlayer
          className="max-w-3xl"
          src={flower}
          poster={gradient}
          title="Ship your first app"
        />
      </Section>

      <Section title="With its title as a caption, and the keys: space or K, the arrows, M and F">
        <VideoPlayer
          className="max-w-3xl"
          src={flower}
          poster={gradient}
          title="Share it with your team"
          showTitle
        />
      </Section>

      <Section title="In another shape, which is 4 / 3 here">
        <VideoPlayer
          className="max-w-md"
          src={flower}
          poster={gradient}
          title="Roll back a bad change"
          aspectRatio="4 / 3"
        />
      </Section>
    </>
  );
}
