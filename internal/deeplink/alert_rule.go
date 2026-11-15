package deeplink

import "github.com/modelcontextprotocol/go-sdk/mcp"

func ToMetaWithLink(link, label string) mcp.Meta {
	meta := ToMeta(link)
	meta["links"] = []map[string]string{{"label": label, "url": link}}
	return meta
}
