-- Reverse 481_hook_fire_history.
--
-- Locks: ACCESS EXCLUSIVE on hook_fire_history while it is dropped; no
-- other table is touched. Expected duration: <1 second.
-- Data loss: UNRECOVERABLE for all stored fire history (this table is the
-- only copy). At stage 1 nothing has been written yet; after stage 7,
-- export the rows before rolling back.

DROP TABLE IF EXISTS hook_fire_history;
