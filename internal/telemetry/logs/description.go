package logs

const GetLogsDescription = `
	Get logs filtered by optional service name and/or severity level within a specified time range. 
	Omitting service returns logs from all services.

    service: (Optional) The name of the service to get the logs for.
	severity: (Optional) The severity of the logs to get.
    limit: (Optional) The maximum number of logs to return. Defaults to 20.
    lookback_minutes: (Recommended) Number of minutes to look back from now. Use this for relative time ranges.
    start_time_iso: (Optional) The start time to get the data from. Leave empty to use lookback_minutes instead.
    end_time_iso: (Optional) The end time to get the data from. Leave empty to default to current time.

	Response is a list of log entries in the 'result' field containing the following fields:
		stream:
			it contains the attributes of the log entry
			the attributes starting with resources_ are the resource attributes of the log entry
			and the rest of the attributes are log attributes 
		values:
			it contains the log message and the timestamp of the log entry
			each value is a list of two elements, the first element is the timestamp and the second element is the log message

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
