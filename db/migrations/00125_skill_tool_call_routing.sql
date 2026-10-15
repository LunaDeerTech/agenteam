-- agenteam:transaction tx
-- +goose Up
-- These routing names refer to the original captured binding. skill.install
-- continues to use the original installation Operation/Attempt ledger.
ALTER TABLE agenteam_execution.tool_calls DROP CONSTRAINT tool_calls_operation_name_check;
ALTER TABLE agenteam_execution.tool_calls ADD CONSTRAINT tool_calls_operation_name_check
 CHECK(operation_name IN ('read-file','write-file','run-command','skill.install','skill.list','skill.read','skill.assign','skill.prepare','task.read','task.transfer','artifact.list','artifact.create','artifact.read','knowledge.query','knowledge.read','memory.retain','memory.recall','memory.reflect','meeting.list','meeting.request','meeting.add-agent','meeting.remove-agent','meeting.update','meeting.comment','meeting.request-decision','mcp.list-resources','mcp.read-resource','mcp.call'));
