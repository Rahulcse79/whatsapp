-- Metadata-only product analytics (T4.03, HLD §18.1). Daily rollup cells only:
-- a (day, metric) grid of counts and computed distinct-user totals (DAU/MAU).
-- There is NO user column and no content — the server cannot analyse what it
-- cannot read, and distinct counting rides a Valkey HyperLogLog sketch, so this
-- table never records WHICH users were active, only how many. Retention ~13
-- months (analytics domain.RollupRetentionDays), trimmed by a daily job.
CREATE TABLE analytics_daily (
    day    date   NOT NULL,
    metric text   NOT NULL,                 -- signups | messages_relayed | calls_started | call_minutes | dau | mau | flag_exposure[:flag]
    value  bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (day, metric)
);

-- Range scans over a metric's history (dashboard/query read path).
CREATE INDEX analytics_daily_by_metric ON analytics_daily (metric, day);
