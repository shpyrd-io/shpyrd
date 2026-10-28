import { afterEach, describe, expect, it, vi } from "vitest";
import { backend, metricsPath, toLogLine, toMetrics, toProject, type DetailAnswer, type MetricsAnswer } from "./backend";

describe("the Backend reads the server's shapes as the screens read them", () => {
  it("lifts the phase, the address, the access and the processes of a project out of status and spec", () => {
    const detail = {
      slug: "shop",
      displayName: "Shop",
      namespace: "p-shop",
      createdAt: "2026-09-30T04:52:58Z",
      spec: { source: { blob: { sha256: "ab1a", ref: "3698" } }, build: { strategy: "buildpacks" }, access: "authenticated", exposure: "internal" },
      status: { phase: "Running", message: "web 1/1", url: "https://shop.shpyrd.test", releases: [{ number: 1, kind: "deploy", createdAt: "2026-09-30T05:07:46Z" }, { number: 2, kind: "config", createdAt: "2026-09-30T05:08:46Z" }] },
      processes: { web: { desired: 2, ready: 2, size: "shared-s" } },
    } as unknown as DetailAnswer;
    const p = toProject(detail);
    expect(p.phase).toBe("Running");
    expect(p.url).toBe("https://shop.shpyrd.test");
    expect(p.access).toBe("authenticated");
    expect(p.exposure).toBe("internal");
    expect(p.release).toBe(2);
    expect(p.spec.processes).toEqual({ web: { size: "shared-s", replicas: 2 } });
  });

  it("takes a project without access, releases or processes as public, at release 0, with nothing to run", () => {
    const p = toProject({ slug: "new", displayName: "New", namespace: "p-new", createdAt: "", spec: {}, status: { phase: "Pending", releases: [] } } as unknown as DetailAnswer);
    expect(p.access).toBe("public");
    expect(p.release).toBe(0);
    expect(p.spec.processes).toEqual({});
  });

  it("reads a log record: the time as a clock, the instance, and the message parsed", () => {
    const line = toLogLine({ t: "2026-09-30T05:07:50Z", i: "web.1", p: "web-abc", m: '{"level":"warn","msg":"slow query","duration_ms":1840}' });
    expect(line.instance).toBe("web.1");
    expect(line.time).toMatch(/^\d\d:\d\d:\d\d$/);
    expect(line.level).toBe("warn");
    expect(line.message).toBe("slow query");
    expect(line.fields?.map((f) => f.key)).toContain("duration_ms");
    expect(line.raw).toContain("slow query");
  });

  it("keeps a plain line plain: no level, the text as the message", () => {
    const line = toLogLine({ i: "web.1", m: "listening on :8080" });
    expect(line.level).toBeUndefined();
    expect(line.message).toBe("listening on :8080");
    expect(line.time).toBeUndefined();
  });

  it("turns the charts into the series the metrics page reads, with the allocation and the span", () => {
    const answer: MetricsAnswer = {
      range: "1h",
      step: 60,
      charts: [
        { id: "latency", title: "Response time", unit: "ms", kind: "line", series: [{ name: "95th percentile", points: [[100, 12], [160, 14]] }] },
        { id: "cpu", title: "CPU", unit: "cores", kind: "line", instanceCapable: true, note: "1 replaced instance hidden", series: [{ name: "web", points: [[100, 0.2], [160, 0.3]], reference: 0.5, burst: 1 }] },
        { id: "memory", title: "Memory", unit: "bytes", kind: "line", series: [], error: "no data" },
      ],
      releases: [{ number: 3, time: 130, label: "v3" }],
      ooms: [{ time: 140, process: "web", instance: "web.1", limit: 268435456, size: "shared-m", release: 3 }],
    };
    const m = toMetrics(answer);
    expect(m.from).toBe(100);
    expect(m.to).toBe(160);
    expect(m.responseTime.series[0]?.name).toBe("95th percentile");
    expect(m.cpu.series[0]).toMatchObject({ name: "web", reference: 0.5, burst: 1, tone: "orange" });
    expect(m.cpu).toMatchObject({ unit: "cores", instanceCapable: true, note: "1 replaced instance hidden" });
    expect(m.responseTime.instanceCapable).toBe(false);
    expect(m.memory.series).toEqual([]);
    expect(m.memory.error).toBe("no data");
    expect(m.network.series).toEqual([]);
    expect(m.throughput.series).toEqual([]);
    expect(m.releases).toEqual([{ time: 130, label: "v3" }]);
    expect(m.ooms).toEqual([{ time: 140, process: "web", instance: "web.1", limit: 268435456, size: "shared-m", release: 3 }]);
  });

  it("reads an answer from a server that knows no kills as none", () => {
    const m = toMetrics({ range: "1h", step: 60, charts: [], releases: [] });
    expect(m.ooms).toEqual([]);
  });

  it("asks the server only for what differs from its own defaults (RFC-0027)", () => {
    expect(metricsPath("shop", { range: "1h", mode: "total" })).toBe("/api/projects/shop/metrics?range=1h&mode=total");
    expect(metricsPath("shop", { range: "1h", by: "process", agg: "none", replaced: false })).toBe("/api/projects/shop/metrics?range=1h");
    expect(metricsPath("shop", { range: "15m", process: "web", by: "instance", agg: "sum", mode: "percent", replaced: true })).toBe(
      "/api/projects/shop/metrics?range=15m&process=web&by=instance&agg=sum&mode=percent&replaced=true",
    );
  });
});

describe("the Backend reads GitHub's answers", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("marks a repository list the server cut, and asks for one account when told", async () => {
    const fetchMock = vi.fn(async (_path: string) => new Response(JSON.stringify([{ installation: 42, fullName: "acme/a", private: false, defaultBranch: "main" }]), { status: 200, headers: { "X-Shpyrd-Truncated": "true" } }));
    vi.stubGlobal("fetch", fetchMock);
    const cut = await backend.githubRepositories(42);
    expect(cut.truncated).toBe(true);
    expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/workspace/github/repositories?installation=42");
    vi.stubGlobal("fetch", vi.fn(async () => new Response("[]", { status: 200 })));
    expect((await backend.githubRepositories()).truncated).toBeUndefined();
  });

  it("marks a branch list the server cut", async () => {
    vi.stubGlobal("fetch", vi.fn(async (_path: string) => new Response('["main"]', { status: 200, headers: { "X-Shpyrd-Truncated": "true" } })));
    expect((await backend.githubBranches("acme/a")).truncated).toBe(true);
  });

  it("takes a 404 on the person's link as no link", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ error: "no link" }), { status: 404 })));
    expect(await backend.githubMe()).toBeNull();
  });
});
