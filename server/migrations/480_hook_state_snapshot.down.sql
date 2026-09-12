-- Reverse 480_hook_state_snapshot.
--
-- Locks: ACCESS EXCLUSIVE on hook_state_snapshot while it is dropped; no
-- other table is touched. Expected duration: <1 second.
-- Data loss: destroys only the snapshot cache; a host refetch rebuilds it
-- (invariant 4). No authoritative state is derived from it, so nothing
-- else is affected.

DROP TABLE IF EXISTS hook_state_snapshot;
