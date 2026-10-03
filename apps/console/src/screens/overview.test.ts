import { describe, expect, it } from "vitest";
import { misplacedByNode } from "./overview";

describe("the platform's pods outside the platform pool", () => {
  it("are told by node, with what made them once and the CPU they reserve there", () => {
    const nodes = misplacedByNode([
      { namespace: "keda", name: "keda-add-ons-http-interceptor-57cb-a", owner: "Deployment/keda-add-ons-http-interceptor", node: "10.0.1.10", pool: "apps", cpu: "50m" },
      { namespace: "keda", name: "keda-add-ons-http-interceptor-57cb-b", owner: "Deployment/keda-add-ons-http-interceptor", node: "10.0.1.10", pool: "apps", cpu: "50m" },
      { namespace: "kube-system", name: "calico-typha-64bd-c", owner: "Deployment/calico-typha", node: "10.0.1.10", pool: "apps" },
      { namespace: "shpyrd-system", name: "shpyrd-server-7485-d", owner: "Deployment/shpyrd-server", node: "10.0.1.228", pool: "data", cpu: "20m" },
      { namespace: "default", name: "stray", node: "10.0.1.228", pool: "data" },
    ]);
    expect(nodes).toEqual([
      { node: "10.0.1.10", pool: "apps", owners: ["keda/keda-add-ons-http-interceptor", "kube-system/calico-typha"], millicores: 100 },
      { node: "10.0.1.228", pool: "data", owners: ["shpyrd-system/shpyrd-server", "default/stray"], millicores: 20 },
    ]);
  });
});
