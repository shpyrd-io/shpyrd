"use client";

import * as React from "react";
import { cn } from "cn";
import { glass } from "../lib/glass";
import { VideoPlayer } from "./video-player";

type MediaItem = {
  id: string;
  title: string;
  src: string;
  poster?: string;
  // As it is to be read, such as `3:42`.
  duration: string;
  description?: string;
};

// A video with the others of its series beside it: the player, larger, and a
// list of the videos, each a row with a small picture, its title, how long it
// runs and a line about it. The one playing is marked with an orange bar and
// a "Now playing" tag. Choosing another, by click or Enter, puts it in the
// player and plays it.
//
// The list scrolls within a height matched to the player's, so the pair stay
// the same size however many videos there are. On a narrow width the list goes
// under the player instead of beside it.
function MediaPlaylist({
  className,
  items,
  initialId,
  ...props
}: React.ComponentProps<"div"> & {
  items: MediaItem[];
  // The video the player opens on. Without it, the first.
  initialId?: string;
}) {
  const [currentId, setCurrentId] = React.useState(initialId ?? items[0]?.id);
  // The player starts by itself only after a choice from the list, never on arrival.
  const [chosen, setChosen] = React.useState(false);
  const current = items.find((item) => item.id === currentId) ?? items[0];
  if (!current) return null;

  return (
    <div
      data-slot="media-playlist"
      className={cn("@container/playlist", className)}
      {...props}
    >
      <div className="grid gap-4 @3xl/playlist:grid-cols-[minmax(0,2fr)_minmax(0,1fr)] @3xl/playlist:items-stretch">
        <VideoPlayer
          key={current.id}
          src={current.src}
          poster={current.poster}
          title={current.title}
          autoPlay={chosen}
          className="self-start"
        />

        {/* The list takes the height of the player beside it, and on a narrow width a fixed one. */}
        <div className="relative @3xl/playlist:min-h-0">
          <ul
            data-slot="media-playlist-list"
            aria-label="Playlist"
            className={cn(
              glass,
              "grid max-h-80 content-start gap-0.5 overflow-y-auto p-2 @3xl/playlist:absolute @3xl/playlist:inset-0 @3xl/playlist:max-h-none",
            )}
          >
            {items.map((item, index) => {
              const on = item.id === current.id;
              return (
                <li key={item.id}>
                  <button
                    type="button"
                    data-slot="media-playlist-item"
                    data-current={on || undefined}
                    aria-current={on ? "true" : undefined}
                    onClick={() => {
                      setChosen(true);
                      setCurrentId(item.id);
                    }}
                    className={cn(
                      "relative flex w-full items-center gap-3 rounded-xl py-2 pr-3 pl-4 text-left outline-none transition-colors hover:bg-foreground/5 focus-visible:ring-2 focus-visible:ring-primary/60",
                      on && "bg-primary/8 hover:bg-primary/10",
                    )}
                  >
                    <span
                      aria-hidden="true"
                      className={cn(
                        "absolute top-2 bottom-2 left-1 w-[3px] rounded-full bg-primary transition-opacity",
                        on ? "opacity-100" : "opacity-0",
                      )}
                    />
                    <span
                      data-slot="media-playlist-thumb"
                      className="flex aspect-video w-20 shrink-0 items-center justify-center overflow-hidden rounded-lg border border-foreground/8 bg-muted bg-cover bg-center font-heading text-sm font-semibold text-muted-foreground dark:border-foreground/15"
                      style={item.poster ? { backgroundImage: `url(${item.poster})` } : undefined}
                    >
                      {!item.poster && index + 1}
                    </span>
                    <span className="grid min-w-0 gap-0.5">
                      <span
                        className={cn(
                          "font-heading text-sm leading-snug font-semibold",
                          on ? "text-primary" : "text-foreground",
                        )}
                      >
                        {item.title}
                      </span>
                      <span className="flex items-center gap-2 text-xs text-muted-foreground">
                        <span className="font-mono tabular-nums">{item.duration}</span>
                        {on && (
                          <span className="font-medium text-primary">Now playing</span>
                        )}
                      </span>
                      {item.description && (
                        <span className="truncate text-xs text-muted-foreground">
                          {item.description}
                        </span>
                      )}
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        </div>
      </div>
    </div>
  );
}

export { MediaPlaylist, type MediaItem };
