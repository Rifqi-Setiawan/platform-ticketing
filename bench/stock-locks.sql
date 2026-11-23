-- Run from a second psql connection during a success-profile benchmark.
-- Each row counts active API connections and those blocked on the stock update.
SELECT clock_timestamp(), count(*) AS active_api_connections,
       count(*) FILTER (WHERE wait_event_type = 'Lock'
                         AND query ILIKE '%UPDATE sections SET available=available-1%') AS stock_lock_waiters
FROM pg_stat_activity
WHERE datname = current_database() AND pid <> pg_backend_pid() AND state = 'active'
  AND backend_type = 'client backend';
\watch 0.1
