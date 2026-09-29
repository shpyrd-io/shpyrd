---
title: Logs
description: Live logs, the log agent that labels and bounds them, and drains that forward them to your provider, per project or for the whole cluster.
---

Every process writes to stdout and stderr; shpyrd names the instance (`web.1`, `worker.2`), streams the lines live, keeps them bounded on the node, and forwards them wherever you keep your logs. {% .lead %}

## Live logs

```shell
shpyrd logs --project shop              # last 200 lines of every instance
shpyrd logs --project shop -f -p worker # follow one process type
```

The dashboard's **Logs** tab streams the same lines with a process filter, a text filter and level highlighting. This path reads from the Kubernetes API and works on every cluster with nothing enabled.

## The log agent

`shpyrd extensions enable logs-agent` runs [Vector](https://vector.dev) on every node ([RFC-0022a](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0022a-log-agent.md)). It reads the container logs of project instances and turns every line into a structured event:

```json
{"time": "...", "project": "shop", "process": "web", "instance": "web.2", "stream": "stdout",
 "level": "info", "msg": "request", "fields": {"method": "GET", "path": "/", "status": 200}}
```

Lines that are JSON get `level` and `msg` promoted and the rest kept in `fields`; plain lines keep `msg` as the text. Build output and the pods of attached databases and caches are not part of this stream.

**Bounded on the node.** Each container keeps at most 20 MiB of logs on disk (two files of 10 MiB, rotated by the kubelet), so an application logging at full speed cannot fill a node. History beyond that lives wherever you drain it.

## Log drains

A drain forwards lines as they are written ([RFC-0023](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0023-log-drains.md)). Two kinds of receiver:

| Receiver | URL | What arrives |
| --- | --- | --- |
| HTTPS (Datadog, Better Stack, Axiom, your own collector) | `https://...` | JSON, one object per line, batched, with the headers you set (API keys) |
| Syslog (Papertrail, rsyslog, a SIEM) | `syslog://host:port` or `syslog+tls://host:port` | RFC 5424 over TCP: the project as APP-NAME, the instance as PROCID, the level as severity |

And two scopes:

- a **project drain** receives that project's lines; project admins add them on the project page or with `--project`;
- a **cluster drain** receives every project's lines, labelled with the project; platform admins add them on the Cluster page or with `--cluster`.

```shell
shpyrd drains add https://in.logs.betterstack.com/ --header "Authorization: Bearer ..." --project shop
shpyrd drains add https://http-intake.logs.datadoghq.com/api/v2/logs --header "DD-API-KEY: ..." --processes web --project shop
shpyrd drains add syslog+tls://logs.papertrailapp.com:6514 --cluster
shpyrd drains list --project shop
shpyrd drains remove in-logs-betterstack-com --project shop
```

The name defaults to the receiver's host. `--processes` limits a drain to some process types. Header values are stored in the cluster and never shown again, in the CLI or the dashboard. Both cards show each drain's delivery status (Pending, Active with the last delivery time and line count, Failing with the error) refreshed every 30 seconds; a receiver that keeps failing raises a `DrainFailing` event visible in the project's activity.

{% callout title="Storage and history" %}
Drains are the foundation for log history too: a cluster drain to Loki (or any receiver that speaks its protocol) plus a query API is [RFC-0022b](https://github.com/shpyrd-io/shpyrd/blob/main/rfcs/0022-log-pipeline.md), an optional add-on. Until then, `--since` in the CLI and a time range in the viewer are not available; what the node keeps is what the live stream shows.
{% /callout %}

### Providers

| Provider | Drain |
| --- | --- |
| Better Stack (Logtail) | `https://in.logs.betterstack.com/` with `Authorization: Bearer <source token>` |
| Datadog | `https://http-intake.logs.datadoghq.com/api/v2/logs` with `DD-API-KEY: <key>` (use your site's intake host) |
| Axiom | `https://api.axiom.co/v1/datasets/<dataset>/ingest` with `Authorization: Bearer <token>` |
| Papertrail | `syslog+tls://logsN.papertrailapp.com:<port>` |
| Grafana Loki (push API) | `https://loki.example.com/loki/api/v1/push` accepts JSON lines only through a proxy; native Loki support is RFC-0022b |
| Anything that accepts newline-delimited JSON over HTTP, or syslog | works |
