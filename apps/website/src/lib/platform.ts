// The computer the page is read on, for the installer to offer. Only what an
// installer exists for: macOS (one universal file), Windows, and Linux on
// x86-64. A phone or a tablet, a Linux on ARM, or anything not recognised is
// null, and gets the manual install.
export type Platform = "mac" | "windows" | "linux";

type Reader = {
  userAgent: string;
  platform?: string;
  maxTouchPoints?: number;
  userAgentData?: { platform?: string; mobile?: boolean };
};

export function platformOf(reader: Reader): Platform | null {
  const ua = reader.userAgent;
  if (reader.userAgentData?.mobile || /iPhone|iPad|iPod|Android/i.test(ua)) return null;
  const said = `${reader.userAgentData?.platform ?? ""} ${reader.platform ?? ""} ${ua}`;
  if (/Mac/i.test(said)) {
    // An iPad asking for the desktop site says it is a Mac; it has a touch screen.
    return (reader.maxTouchPoints ?? 0) > 1 ? null : "mac";
  }
  if (/Win/i.test(said)) return "windows";
  if (/Linux|X11|CrOS/i.test(said)) return /aarch64|arm/i.test(said) ? null : "linux";
  return null;
}
