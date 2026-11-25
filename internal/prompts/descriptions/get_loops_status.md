Check whether Loops is enabled for this organization. Call before create or list tools when enablement is unknown.

Returns `{"enabled": true}` when Loops is provisioned, or `{"enabled": false, "reason": "LOOPS_NOT_ENABLED", "message": "..."}` with an actionable next step. Does not create or change loops.