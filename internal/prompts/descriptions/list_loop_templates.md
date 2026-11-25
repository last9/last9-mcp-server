List Loops templates agents can create from (launch jobs and ops pack). Prefer a template over a blank graph.

Returns `{"templates": [{"slug", "name", "required_params", "connectors", "capability"}], "count"}`. Pass optional `capability` to narrow. Pair with `create_loop` (later) using a template `slug` and required params; never invent Slack channels or recipients.