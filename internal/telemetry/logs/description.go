package logs

const GetLogsDescription = `
	Get logs for service or group of services using JSON pipeline queries for advanced filtering, parsing, aggregation, and processing. 
	
	This tool requires the logjson_query parameter which contains a JSON pipeline query. Use the logjson_query_builder prompt to generate these queries from natural language descriptions.

	Parameters:
	- logjson_query: (Required) JSON pipeline query array for advanced log filtering and processing. Use logjson_query_builder prompt to generate from natural language.
	- lookback_minutes: (Optional) Number of minutes to look back from now. Default: 60 minutes.
	- start_time_iso: (Optional) Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to use lookback_minutes.
	- end_time_iso: (Optional) End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.

	The logjson_query supports:
	- Filter operations: Filter logs based on conditions
	- Parse operations: Parse log content (json, regexp, logfmt)
	- Aggregate operations: Perform aggregations (sum, avg, count, etc.)
	- Window aggregate operations: Time-windowed aggregations
	- Transform operations: Transform/extract fields
	- Select operations: Select specific fields and apply limits

	Response contains the results of the JSON pipeline query execution.

`

const GetDropRulesDescription = `Retrieve and display the configured drop rules for log management in Last9.
Drop rules are filtering mechanisms that determine which logs are excluded from being processed and stored.`

const AddDropRuleDescription = `
	Add Drop Rule filtering capabilities, it supports filtering on metadata about the logs, 
	not the actual log content itself. 
	
	Not Supported
	- Key:
		- filtering on message content in the values array is not supported
		- Message (attributes[\"message\"])
		- Body (attributes[\"body\"])
		- Individual keys like key1, key2, etc.
		- Regular expression patterns
		- Actual log content in values object

	- Operators:
		- No partial matching
		- No contains, startswith, or endswith operators
		- No numeric comparisons (greater than, less than)

	- Conjunctions:
		- No or logic between filters

	Supported
	- Key:
		- Log attributes (attributes[\"key_name\"])
		- Resource attributes (resource.attributes[\"key_name\"])

	- Operators:
		- equals
		- not_equals

	- Logical Conjunctions:
		- and

	Key Requirements
	- All attribute keys must use proper escaping with double quotes
	- Resource attributes must be prefixed with resource.attributes
	- Log attributes must be prefixed with attributes
	- Each filter requires a conjunction (and) to combine with other filters

	The system only supports filtering on metadata about the logs, not the actual log content itself.
`
