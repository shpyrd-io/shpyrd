"use client";

import * as React from "react";
import { cn } from "cn";
import { FileIcon, ImageIcon, Upload } from "lucide-react";
import { Button } from "./button";

// A place a file is dropped on, or chosen from by a click. With a file
// already there it shows it, an image as itself, and offers to change
// or remove it. What does not fit, by type or by size, is refused with
// a word under the drop.

// "256 KB", "1.2 MB".
export function fileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${Math.round((bytes / (1024 * 1024)) * 10) / 10} MB`;
}

// Whether a file is of the kinds `accept` names, as an input's accept:
// mime types, wildcards such as image/*, and extensions such as .svg.
export function accepts(file: { name: string; type: string }, accept?: string): boolean {
  if (!accept) return true;
  return accept
    .split(",")
    .map((a) => a.trim().toLowerCase())
    .filter(Boolean)
    .some((a) => {
      if (a.startsWith(".")) return file.name.toLowerCase().endsWith(a);
      if (a.endsWith("/*")) return file.type.toLowerCase().startsWith(a.slice(0, -1));
      return file.type.toLowerCase() === a;
    });
}

function FileDrop({
  className,
  accept,
  maxSize,
  preview,
  fileName,
  onChange,
  onRemove,
  onReject,
  disabled = false,
  invite = "Drop a file here, or",
  choose = "Choose one",
  ...props
}: Omit<React.ComponentProps<"div">, "onChange"> & {
  // The kinds taken, as an input's accept: "image/png,.svg".
  accept?: string;
  // The most bytes a file may have.
  maxSize?: number;
  // What is there now: the address of an image, shown as itself.
  preview?: string;
  // What is there now, when it is not an image: its name.
  fileName?: string;
  onChange: (file: File) => void;
  // With it, what is there can be removed.
  onRemove?: () => void;
  // Why a file was refused, besides the word under the drop.
  onReject?: (reason: string) => void;
  disabled?: boolean;
  // The words on an empty drop.
  invite?: React.ReactNode;
  choose?: React.ReactNode;
}) {
  const input = React.useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = React.useState(false);
  const [reason, setReason] = React.useState<string | null>(null);
  const has = !!preview || !!fileName;

  const take = (file: File | undefined) => {
    if (!file || disabled) return;
    let why: string | null = null;
    if (!accepts(file, accept)) why = `${file.name} is not a kind that is taken here.`;
    else if (maxSize !== undefined && file.size > maxSize) why = `${file.name} is ${fileSize(file.size)}; at most ${fileSize(maxSize)} is taken.`;
    setReason(why);
    if (why) onReject?.(why);
    else onChange(file);
  };
  const open = () => {
    if (!disabled) input.current?.click();
  };

  return (
    <div data-slot="file-drop" className={cn("grid gap-1.5", className)} {...props}>
      <div
        role="button"
        tabIndex={disabled ? -1 : 0}
        aria-disabled={disabled || undefined}
        aria-label={has ? "Change the file" : "Choose a file"}
        data-dragging={dragging || undefined}
        data-has-file={has || undefined}
        className={cn(
          "relative flex min-h-28 items-center justify-center gap-4 rounded-md border border-dashed border-input bg-muted/30 px-4 py-4 text-sm outline-none transition-colors",
          "focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50",
          !disabled && "cursor-pointer hover:border-ring/60 hover:bg-muted/50",
          dragging && "border-primary bg-primary/5",
          disabled && "cursor-not-allowed opacity-50",
        )}
        onClick={open}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            open();
          }
        }}
        onDragEnter={(e) => {
          e.preventDefault();
          if (!disabled) setDragging(true);
        }}
        onDragOver={(e) => {
          e.preventDefault();
          if (!disabled) setDragging(true);
        }}
        onDragLeave={(e) => {
          if (e.currentTarget.contains(e.relatedTarget as Node | null)) return;
          setDragging(false);
        }}
        onDrop={(e) => {
          e.preventDefault();
          setDragging(false);
          take(e.dataTransfer.files?.[0]);
        }}
      >
        <input
          ref={input}
          type="file"
          accept={accept}
          disabled={disabled}
          className="sr-only"
          tabIndex={-1}
          onChange={(e) => {
            take(e.target.files?.[0]);
            e.target.value = "";
          }}
        />
        {has ? (
          <>
            {preview ? (
              <img src={preview} alt="" className="max-h-16 max-w-40 shrink-0 object-contain" />
            ) : (
              <span className="flex size-12 shrink-0 items-center justify-center rounded-md border bg-background text-muted-foreground">
                <FileIcon className="size-5" />
              </span>
            )}
            <div className="grid min-w-0 flex-1 gap-1">
              {fileName && <div className="truncate font-medium">{fileName}</div>}
              <div className="text-xs text-muted-foreground">{dragging ? "Drop it to replace what is there." : "Drop another here, or"}</div>
              <div className="flex flex-wrap gap-2" onClick={(e) => e.stopPropagation()}>
                <Button type="button" size="xs" variant="outline" disabled={disabled} onClick={open}>
                  Change
                </Button>
                {onRemove && (
                  <Button type="button" size="xs" variant="ghost" disabled={disabled} onClick={onRemove}>
                    Remove
                  </Button>
                )}
              </div>
            </div>
          </>
        ) : (
          <div className="grid justify-items-center gap-2 text-center">
            <span className="flex size-10 items-center justify-center rounded-full bg-background text-muted-foreground ring-1 ring-border">
              {accept?.startsWith("image") ? <ImageIcon className="size-5" /> : <Upload className="size-5" />}
            </span>
            <div className="text-muted-foreground">
              {dragging ? "Drop it." : invite}{" "}
              {!dragging && (
                <span className="font-medium text-foreground underline underline-offset-4">{choose}</span>
              )}
            </div>
          </div>
        )}
      </div>
      {reason && (
        <p role="alert" className="text-xs text-destructive">
          {reason}
        </p>
      )}
    </div>
  );
}

export { FileDrop };
