-- Every container killed for crossing its memory limit (#119). Kubernetes
-- keeps only each container's last death and the node's events expire
-- within the hour, so the controller records each one here as it sees it.
-- workspace_id and project are empty for a platform component (the backup
-- CronJob in the system namespace).
CREATE TABLE oom_kills (
    namespace      TEXT NOT NULL,
    pod            TEXT NOT NULL,
    container      TEXT NOT NULL,
    at             TIMESTAMPTZ NOT NULL,   -- the kubelet's finishedAt
    workspace_id   TEXT NOT NULL DEFAULT '',
    project        TEXT NOT NULL DEFAULT '', -- the project's short ID
    process        TEXT NOT NULL DEFAULT '', -- web, worker, release, build, run
    instance       TEXT NOT NULL DEFAULT '', -- the pod's human name then: web.1
    limit_bytes    BIGINT NOT NULL DEFAULT 0,
    size_name      TEXT NOT NULL DEFAULT '',
    release_number INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (namespace, pod, container, at)
);
CREATE INDEX oom_kills_namespace_at ON oom_kills (namespace, at DESC);
CREATE INDEX oom_kills_at ON oom_kills (at DESC);
