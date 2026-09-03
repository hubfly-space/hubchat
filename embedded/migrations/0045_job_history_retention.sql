-- Keep the hourly terminal-job retention sweep bounded and index-backed.
CREATE INDEX jobs_terminal_finished_at
    ON jobs (finished_at, id)
    WHERE state IN ('succeeded', 'failed', 'dead', 'cancelled');
