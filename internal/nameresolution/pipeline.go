package nameresolution

type ServicePipeline struct {
	Requested string
	Scope     []map[string]any
	pipeline  []map[string]any
	target    []any
}

func PrepareServicePipeline(pipeline []map[string]any) (ServicePipeline, bool) {
	p := ServicePipeline{pipeline: clonePipeline(pipeline)}
	var conditions []any
	for _, stage := range p.pipeline {
		if stage["type"] != "filter" && stage["type"] != "where" {
			break
		}
		if !p.collect(stage["query"], &conditions) {
			return ServicePipeline{}, false
		}
	}
	if p.target == nil {
		return ServicePipeline{}, false
	}
	p.Requested, _ = p.target[1].(string)
	if p.Requested == "" {
		return ServicePipeline{}, false
	}
	query := map[string]any{}
	if len(conditions) > 0 {
		query["$and"] = conditions
	}
	p.Scope = []map[string]any{{"type": "filter", "query": query}}
	return p, true
}

func (p *ServicePipeline) collect(value any, scope *[]any) bool {
	condition, ok := value.(map[string]any)
	if !ok {
		return false
	}
	if len(condition) == 0 {
		return true
	}
	if len(condition) != 1 {
		return false
	}
	for op, value := range condition {
		args, ok := value.([]any)
		if !ok {
			return false
		}
		if op == "$and" {
			for _, child := range args {
				if !p.collect(child, scope) {
					return false
				}
			}
			return true
		}
		if len(args) != 2 {
			return false
		}
		field, ok := args[0].(string)
		if !ok {
			return false
		}
		if field == "ServiceName" {
			if op != "$eq" || p.target != nil {
				return false
			}
			if _, ok := args[1].(string); !ok {
				return false
			}
			p.target = args
		}
		switch field {
		case "resources['deployment.environment']", "resources['deployment.environment.name']":
			*scope = append(*scope, condition)
		}
	}
	return true
}

func (p ServicePipeline) WithName(name string) []map[string]any {
	copy, ok := PrepareServicePipeline(p.pipeline)
	if !ok {
		return clonePipeline(p.pipeline)
	}
	copy.target[1] = name
	return copy.pipeline
}

func clonePipeline(pipeline []map[string]any) []map[string]any {
	out := make([]map[string]any, len(pipeline))
	for i, stage := range pipeline {
		out[i] = cloneValue(stage).(map[string]any)
	}
	return out
}

func cloneValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = cloneValue(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneValue(item)
		}
		return out
	default:
		return value
	}
}
