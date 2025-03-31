package logs

const GetLogsDescription = `
	Get logs filtered by optional service name and/or severity level within a specified time range. 
	Omitting service returns logs from all services.

    service: (Optional) The name of the service to get the logs for.
	severity: (Optional) The severity of the logs to get.
    limit: (Optional) The maximum number of logs to return. Defaults to 20.
    start_time_iso: (Optional) The start time to get the data from. Defaults to now.
    end_time_iso: (Optional) The end time to get the data from. Defaults to now.

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
	Add a new drop rule for logs.

	Args:
		The name of the drop rule.
		name: string
		
		A list of filter conditions to apply for this drop rule.
		filters: []struct {
			The key to filter on. If it is a resource attribute, specify it as resource.attribute[key_name].
			Ensure that double quotes in the key name are properly escaped.
			key: string
			
			The value to filter against.
			value: string
			
			The operator used for filtering. Accepted operators include:
			- "equals"
			- "not_equals"
			operator: string
			
			The logical conjunction to apply between filters. Supported conjunction:
			- "and"
			conjunction: string
		}
		
		Example filter configuration:
		{
		    "key": "attributes[\"logtag\"]",
		    "value": "P",
		    "operator": "equals",
		    "conjunction": "and"
		}
	Response:
		List of all drop rules.
`
