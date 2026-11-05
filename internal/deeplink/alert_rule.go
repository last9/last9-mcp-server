package deeplink

import (
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (b *Builder) BuildAlertRuleLink(entityID, ruleID string) string {
	params := url.Values{}
	params.Set("rule", ruleID)
	params.Set("group_id", entityID)
	return b.BuildAlertingGroupsLink() + "?" + params.Encode()
}

func ToMetaWithLink(link, label string) mcp.Meta {
	meta := ToMeta(link)
	meta["links"] = []map[string]string{{"label": label, "url": link}}
	return meta
}
