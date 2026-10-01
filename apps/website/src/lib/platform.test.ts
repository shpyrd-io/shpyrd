import { describe, expect, it } from "vitest";
import { platformOf } from "./platform";

const mac = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15";
const windows = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36";
const linux = "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0";

describe("platformOf", () => {
  it("knows a Mac, a Windows and a Linux computer", () => {
    expect(platformOf({ userAgent: mac, platform: "MacIntel" })).toBe("mac");
    expect(platformOf({ userAgent: windows, platform: "Win32" })).toBe("windows");
    expect(platformOf({ userAgent: linux, platform: "Linux x86_64" })).toBe("linux");
  });

  it("prefers what the browser's client hints say", () => {
    expect(platformOf({ userAgent: "Mozilla/5.0", userAgentData: { platform: "Windows" } })).toBe("windows");
  });

  it("offers nothing to a phone or a tablet", () => {
    expect(platformOf({ userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)" })).toBeNull();
    expect(platformOf({ userAgent: "Mozilla/5.0 (Linux; Android 15; Pixel 9)" })).toBeNull();
    expect(platformOf({ userAgent: mac, platform: "MacIntel", maxTouchPoints: 5 })).toBeNull();
  });

  it("offers nothing to a Linux on ARM, which has no installer", () => {
    expect(platformOf({ userAgent: "Mozilla/5.0 (X11; Linux aarch64)" })).toBeNull();
  });
});
