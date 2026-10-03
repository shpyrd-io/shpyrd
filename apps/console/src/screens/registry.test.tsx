import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import type { RegistryInfo } from "@/api/types";
import { Registry } from "./registry";

const state = vi.hoisted(() => ({ data: {} as RegistryInfo }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: state.data }),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  useMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));

describe("the registry's cloud storage", () => {
  it("names the bucket and shows usage without a volume capacity", () => {
    state.data = {
      mode: "in-cluster", host: "registry.internal", ready: true, tls: true,
      storage: { backend: "s3", bucket: "images", endpoint: "https://objects.example.com", usedBytes: 5 * 2 ** 30, capacityBytes: 0, size: "" },
    };
    const html = renderToStaticMarkup(<Registry />);
    expect(html).toContain("s3://images/docker/");
    expect(html).toContain("https://objects.example.com");
    expect(html).toContain("5.0 GiB stored");
    expect(html).not.toContain("asked for");
    expect(html).not.toContain("Nearly full");
  });

  it("distinguishes an unavailable measurement from an empty bucket", () => {
    state.data = {
      mode: "in-cluster", host: "registry.internal", ready: true, tls: true,
      storage: { backend: "s3", bucket: "images", usedBytes: 0, capacityBytes: 0, size: "", error: "AccessDenied" },
    };
    const html = renderToStaticMarkup(<Registry />);
    expect(html).toContain("Storage usage is unavailable.");
    expect(html).not.toContain("0 B stored");
  });
});
