"use client";

import * as React from "react";
import { cn } from "cn";
import { Search } from "lucide-react";
import { appIconGroups, symbolColours, symbolInk, tileOf, type IconChoice } from "./app-icons";
import { FileDrop, fileSize } from "./file-drop";
import { Input } from "./input";
import { Tile } from "./launcher-card";

// The icon of a card, chosen: a symbol of the set and its colour, or an
// image of its own. What the card will look like is drawn beside the
// colours, as it changes.

// The kinds of image taken, by type and by extension: some systems give
// an SVG no type.
const kinds: Record<string, string> = { ".svg": "image/svg+xml", ".png": "image/png", ".webp": "image/webp" };
const accept = [...Object.values(kinds), ...Object.keys(kinds)].join(",");

function typeOf(file: File): string {
  if (Object.values(kinds).includes(file.type)) return file.type;
  const ext = file.name.slice(file.name.lastIndexOf(".")).toLowerCase();
  return kinds[ext] ?? file.type;
}

function IconPicker({
  className,
  value,
  onChange,
  maxSize = 128 * 1024,
  ...props
}: Omit<React.ComponentProps<"div">, "onChange"> & {
  value: IconChoice;
  onChange: (next: IconChoice) => void;
  // The most bytes an image of its own may have.
  maxSize?: number;
}) {
  const [query, setQuery] = React.useState("");
  const q = query.trim().toLowerCase();
  const groups = appIconGroups
    .map((g) => ({ ...g, icons: g.icons.filter(([name]) => !q || name.includes(q) || g.title.toLowerCase().includes(q)) }))
    .filter((g) => g.icons.length > 0);
  const picture = value.file && value.file.type !== "image/svg+xml";
  const take = (file: File) => {
    const type = typeOf(file);
    const reader = new FileReader();
    reader.onload = () => {
      // The data URL carries the type the file was read with; an SVG
      // without one is given its own.
      const src = String(reader.result).replace(/^data:[^;]*;/, `data:${type};`);
      onChange({ ...value, file: { src, type } });
    };
    reader.readAsDataURL(file);
  };

  return (
    <div data-slot="icon-picker" className={cn("grid gap-5", className)} {...props}>
      <div className="flex items-start gap-4">
        <span style={symbolInk(value.colour)}>
          <Tile {...tileOf(value)} className="size-16 [&_[data-slot=app-symbol]]:size-8 [&_svg]:size-8" />
        </span>
        <div className="grid min-w-0 flex-1 gap-2">
          <div className="text-sm font-medium">Colour</div>
          <div role="radiogroup" aria-label="Colour" className="flex flex-wrap gap-1.5">
            {symbolColours.map((colour) => (
              <button
                key={colour}
                type="button"
                role="radio"
                aria-checked={value.colour === colour}
                aria-label={colour}
                title={colour}
                onClick={() => onChange({ ...value, colour })}
                className="size-6 rounded-full ring-offset-2 ring-offset-background outline-none hover:ring-2 hover:ring-foreground/30 focus-visible:ring-3 focus-visible:ring-ring/50 aria-checked:ring-2 aria-checked:ring-foreground"
                style={{ backgroundColor: `var(--symbol-${colour})` }}
              />
            ))}
          </div>
          {picture && <p className="text-xs text-muted-foreground">A PNG or a WebP is shown as it is: the colour is for a symbol.</p>}
        </div>
      </div>

      <div className="grid gap-2">
        <div className="flex items-center justify-between gap-3">
          <div className="text-sm font-medium">Symbol</div>
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search"
            aria-label="Search the symbols"
            icon={<Search />}
            className="max-w-48"
          />
        </div>
        <div role="radiogroup" aria-label="Symbol" className="grid max-h-64 gap-3 overflow-y-auto rounded-md p-1 ring-1 ring-foreground/10" style={symbolInk(value.colour)}>
          {groups.length === 0 && <p className="p-3 text-sm text-muted-foreground">No symbol is called that.</p>}
          {groups.map((group) => (
            <div key={group.title} className="grid gap-1.5">
              <div className="px-1 pt-1 text-xs text-muted-foreground">{group.title}</div>
              <div className="grid grid-cols-[repeat(auto-fill,minmax(2.5rem,1fr))] gap-1">
                {group.icons.map(([name, Icon]) => {
                  const chosen = !value.file && value.icon === name;
                  return (
                    <button
                      key={name}
                      type="button"
                      role="radio"
                      aria-checked={chosen}
                      aria-label={name}
                      title={name}
                      onClick={() => onChange({ ...value, icon: name, file: undefined })}
                      className={cn(
                        "inline-flex aspect-square items-center justify-center rounded-md text-muted-foreground outline-none hover:bg-muted hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50 [&_svg]:size-5",
                        chosen && "bg-(--ink)/15 text-(--ink) ring-2 ring-(--ink) hover:bg-(--ink)/15 hover:text-(--ink)",
                      )}
                    >
                      <Icon />
                    </button>
                  );
                })}
              </div>
            </div>
          ))}
        </div>
      </div>

      <div className="grid gap-2">
        <div className="grid gap-0.5">
          <div className="text-sm font-medium">Or one of its own</div>
          <p className="text-xs text-muted-foreground">An SVG takes the colour; a PNG or a WebP is shown as it is. Up to {fileSize(maxSize)}.</p>
        </div>
        <FileDrop
          accept={accept}
          maxSize={maxSize}
          preview={value.file?.src}
          onChange={take}
          onRemove={value.file ? () => onChange({ ...value, file: undefined }) : undefined}
          invite="Drop an image here, or"
        />
      </div>
    </div>
  );
}

export { IconPicker };
