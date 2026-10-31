Send feedback about this Last9 MCP server to the Last9 team, who read every report and fix the tools from them. Call it when a Last9 tool errored unexpectedly, returned data that looked wrong, could not do what the user needed, or had a description that misled you. Send it once per problem, then carry on with the user's task; it does not change anything in the user's organization.

Parameters:
- `category`: (Required) `bug` (a tool errored or broke), `wrong_result` (the answer looked incorrect), `missing_capability` (no tool or parameter could do what was needed), `confusing_description` (a description or error message misled you), `slow`, `praise`, or `other`.
- `summary`: (Required) One sentence, at most 300 characters: what you tried, what happened, what you expected.
- `details`: (Optional) At most 4000 characters: the arguments you sent, the error or output, and what you tried instead.
- `tool_name`: (Optional) The Last9 tool the feedback is about, e.g. `get_logs`.

Never include secrets, tokens, passwords, raw log lines, personal data or other customer data: describe the shape of the problem, not the user's data. Your client's name and version and this server's version are added automatically.

Returns JSON with `feedback_id`.