
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
