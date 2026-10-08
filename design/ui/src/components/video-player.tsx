"use client";

import * as React from "react";
import { cn } from "cn";
import { Maximize, Minimize, Pause, Play, Volume2, VolumeX } from "lucide-react";

// Minutes and seconds, as a clock reads them: `1:05`, or `1:02:03` for an hour.
function clock(seconds: number) {
  if (!Number.isFinite(seconds) || seconds < 0) seconds = 0;
  const s = Math.floor(seconds % 60);
  const m = Math.floor((seconds / 60) % 60);
  const h = Math.floor(seconds / 3600);
  const pad = (n: number) => String(n).padStart(2, "0");
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`;
}

// How far a seek by the arrow keys goes, in seconds.
const STEP = 5;

// A small button on the bar of controls: white on the dark of the bar,
// whatever the theme, because the bar lies over a picture.
const barButton =
  "inline-flex size-8 shrink-0 items-center justify-center rounded-full text-white outline-none transition-colors hover:bg-white/15 focus-visible:ring-2 focus-visible:ring-white/70 [&_svg]:size-[18px]";

// A video with controls of its own, drawn to match the rest of the site in
// place of the browser's. Until it plays, a large orange button sits over the
// poster; after, a bar of controls shows when the pointer or focus is on the
// player, and stays while it is paused.
//
// Space or K plays and pauses, the arrows seek by five seconds, M mutes and
// F goes full screen. The progress bar is a slider, so it is seekable by
// click and by keyboard, and read out to a screen reader.
//
// The page picks nothing to start by itself: a video plays only when asked,
// unless `autoPlay` is set, and then not for someone who asked for less motion.
function VideoPlayer({
  className,
  src,
  poster,
  title,
  captions,
  captionsLabel = "English",
  captionsLang = "en",
  aspectRatio = "16 / 9",
  showTitle = false,
  autoPlay = false,
  ...props
}: Omit<React.ComponentProps<"div">, "title"> & {
  src: string;
  // A picture shown before it plays. Anything that is a CSS `background`
  // also does, such as a gradient, so the frame is never empty.
  poster?: string;
  // What the video is called, for those who cannot see it.
  title: string;
  // A WebVTT file of captions, when there is one.
  captions?: string;
  captionsLabel?: string;
  captionsLang?: string;
  // `16 / 9`, `4 / 3`, or a number.
  aspectRatio?: string | number;
  // The title on a bar over the top of the frame, as a caption.
  showTitle?: boolean;
  autoPlay?: boolean;
}) {
  const frame = React.useRef<HTMLDivElement>(null);
  const video = React.useRef<HTMLVideoElement>(null);
  const [playing, setPlaying] = React.useState(false);
  const [started, setStarted] = React.useState(false);
  const [time, setTime] = React.useState(0);
  const [duration, setDuration] = React.useState(0);
  const [muted, setMuted] = React.useState(false);
  const [full, setFull] = React.useState(false);
  const [failed, setFailed] = React.useState(false);

  const isImage = poster && /^(https?:|\/|data:|blob:|\.)/.test(poster);

  const toggle = React.useCallback(() => {
    const el = video.current;
    if (!el) return;
    if (el.paused) void el.play().catch(() => undefined);
    else el.pause();
  }, []);

  const seekTo = React.useCallback((to: number) => {
    const el = video.current;
    if (!el || !Number.isFinite(el.duration)) return;
    el.currentTime = Math.min(el.duration, Math.max(0, to));
  }, []);

  const toggleFull = React.useCallback(() => {
    if (document.fullscreenElement) void document.exitFullscreen();
    else void frame.current?.requestFullscreen?.();
  }, []);

  React.useEffect(() => {
    const onChange = () => setFull(document.fullscreenElement === frame.current);
    document.addEventListener("fullscreenchange", onChange);
    return () => document.removeEventListener("fullscreenchange", onChange);
  }, []);

  React.useEffect(() => {
    if (!autoPlay) return;
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    void video.current?.play().catch(() => undefined);
  }, [autoPlay, src]);

  function onKeyDown(event: React.KeyboardEvent) {
    // A key meant for a button or the slider is theirs.
    const target = event.target as HTMLElement;
    const own = target.closest("button, [role='slider']");
    if (event.altKey || event.ctrlKey || event.metaKey) return;
    const key = event.key.toLowerCase();
    if ((key === " " || key === "enter") && own) return;
    if (own?.getAttribute("role") === "slider" && key.startsWith("arrow")) return;
    if (key === " " || key === "k") toggle();
    else if (key === "arrowleft") seekTo((video.current?.currentTime ?? 0) - STEP);
    else if (key === "arrowright") seekTo((video.current?.currentTime ?? 0) + STEP);
    else if (key === "m" && video.current) video.current.muted = !video.current.muted;
    else if (key === "f") toggleFull();
    else return;
    event.preventDefault();
  }

  function onSliderKey(event: React.KeyboardEvent) {
    const now = video.current?.currentTime ?? 0;
    const to =
      event.key === "ArrowLeft" || event.key === "ArrowDown"
        ? now - STEP
        : event.key === "ArrowRight" || event.key === "ArrowUp"
          ? now + STEP
          : event.key === "Home"
            ? 0
            : event.key === "End"
              ? duration
              : null;
    if (to === null) return;
    event.preventDefault();
    event.stopPropagation();
    seekTo(to);
  }

  function onSliderPointer(event: React.PointerEvent<HTMLDivElement>) {
    const box = event.currentTarget.getBoundingClientRect();
    const seek = (clientX: number) => seekTo(((clientX - box.left) / box.width) * duration);
    seek(event.clientX);
    event.currentTarget.setPointerCapture(event.pointerId);
    const move = (e: PointerEvent) => seek(e.clientX);
    const up = () => {
      event.currentTarget.removeEventListener("pointermove", move);
      event.currentTarget.removeEventListener("pointerup", up);
    };
    event.currentTarget.addEventListener("pointermove", move);
    event.currentTarget.addEventListener("pointerup", up);
  }

  const share = duration > 0 ? Math.min(100, (time / duration) * 100) : 0;

  return (
    <div
      ref={frame}
      data-slot="video-player"
      data-playing={playing || undefined}
      role="group"
      aria-roledescription="video player"
      aria-label={title}
      tabIndex={-1}
      onKeyDown={onKeyDown}
      style={{ aspectRatio }}
      className={cn(
        "group/player relative isolate w-full overflow-hidden rounded-[14px] border border-white/80 bg-black shadow-[0_10px_30px_rgb(20_20_30/0.12)] outline-none dark:border-foreground/15 dark:shadow-[0_10px_30px_rgb(0_0_0/0.6)]",
        full && "rounded-none border-0",
        className,
      )}
      {...props}
    >
      <video
        ref={video}
        data-slot="video-player-video"
        src={src}
        poster={isImage ? poster : undefined}
        preload="metadata"
        playsInline
        onClick={toggle}
        onPlay={() => {
          setPlaying(true);
          setStarted(true);
        }}
        onPause={() => setPlaying(false)}
        onEnded={() => setPlaying(false)}
        onTimeUpdate={(e) => setTime(e.currentTarget.currentTime)}
        onLoadedMetadata={(e) => setDuration(e.currentTarget.duration)}
        onDurationChange={(e) => setDuration(e.currentTarget.duration)}
        onVolumeChange={(e) => setMuted(e.currentTarget.muted)}
        onError={() => setFailed(true)}
        className="absolute inset-0 size-full object-contain"
      >
        {captions && (
          <track kind="captions" src={captions} srcLang={captionsLang} label={captionsLabel} />
        )}
      </video>

      {/* A gradient or a colour for a poster, behind the video until it plays. */}
      {poster && !isImage && !started && (
        <div
          aria-hidden="true"
          data-slot="video-player-poster"
          className="pointer-events-none absolute inset-0 -z-10"
          style={{ background: poster }}
        />
      )}

      {showTitle && (
        <div
          data-slot="video-player-title"
          className={cn(
            "pointer-events-none absolute inset-x-0 top-0 bg-linear-to-b from-black/60 to-transparent px-4 pt-3 pb-8 font-heading text-sm font-medium text-white transition-opacity",
            playing && "opacity-0 group-hover/player:opacity-100 group-focus-within/player:opacity-100",
          )}
        >
          {title}
        </div>
      )}

      {failed && (
        <p
          role="status"
          className="absolute inset-x-0 top-1/2 -translate-y-1/2 px-6 text-center text-sm text-white/80"
        >
          This video could not be loaded.
        </p>
      )}

      {!playing && !failed && (
        <button
          type="button"
          data-slot="video-player-play"
          aria-label={`Play ${title}`}
          onClick={toggle}
          className="absolute top-1/2 left-1/2 flex size-16 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full bg-primary text-white shadow-[0_8px_24px_-6px_rgb(255_79_0/0.7)] outline-none transition-[background-color,scale] hover:bg-primary/90 hover:scale-105 focus-visible:ring-3 focus-visible:ring-white/70 motion-reduce:transition-none motion-reduce:hover:scale-100 [&_svg]:size-7 [&_svg]:fill-current"
        >
          <Play className="translate-x-0.5" aria-hidden="true" />
        </button>
      )}

      <div
        data-slot="video-player-controls"
        className={cn(
          "absolute inset-x-0 bottom-0 grid gap-1 bg-linear-to-t from-black/75 via-black/35 to-transparent px-3 pt-10 pb-2 text-white transition-opacity motion-reduce:transition-none",
          playing
            ? "opacity-0 group-hover/player:opacity-100 group-focus-within/player:opacity-100"
            : "opacity-100",
        )}
      >
        <div
          role="slider"
          tabIndex={0}
          aria-label="Seek"
          aria-valuemin={0}
          aria-valuemax={Math.round(duration) || 0}
          aria-valuenow={Math.round(time)}
          aria-valuetext={`${clock(time)} of ${clock(duration)}`}
          data-slot="video-player-progress"
          onPointerDown={onSliderPointer}
          onKeyDown={onSliderKey}
          className="group/bar relative flex h-4 cursor-pointer touch-none items-center outline-none"
        >
          <div className="relative h-1 w-full rounded-full bg-white/30 transition-[height] group-hover/bar:h-1.5 group-focus-visible/bar:h-1.5 group-focus-visible/bar:ring-2 group-focus-visible/bar:ring-white/70 group-focus-visible/bar:ring-offset-2 group-focus-visible/bar:ring-offset-transparent">
            <div className="absolute inset-y-0 left-0 rounded-full bg-primary" style={{ width: `${share}%` }} />
            <div
              className="absolute top-1/2 size-3 -translate-x-1/2 -translate-y-1/2 rounded-full bg-primary opacity-0 transition-opacity group-hover/bar:opacity-100 group-focus-visible/bar:opacity-100"
              style={{ left: `${share}%` }}
            />
          </div>
        </div>

        <div className="flex items-center gap-1">
          <button
            type="button"
            className={barButton}
            onClick={toggle}
            aria-label={playing ? "Pause" : "Play"}
          >
            {playing ? <Pause className="fill-current" /> : <Play className="fill-current" />}
          </button>
          <button
            type="button"
            className={barButton}
            onClick={() => video.current && (video.current.muted = !video.current.muted)}
            aria-label={muted ? "Unmute" : "Mute"}
            aria-pressed={muted}
          >
            {muted ? <VolumeX /> : <Volume2 />}
          </button>
          <span className="ml-1 font-mono text-xs tabular-nums whitespace-nowrap text-white/90">
            {clock(time)} / {clock(duration)}
          </span>
          <button
            type="button"
            className={cn(barButton, "ml-auto")}
            onClick={toggleFull}
            aria-label={full ? "Exit full screen" : "Full screen"}
          >
            {full ? <Minimize /> : <Maximize />}
          </button>
        </div>
      </div>
    </div>
  );
}

export { VideoPlayer };
