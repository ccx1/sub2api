package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
)

type ProxyGroup struct{ ent.Schema }

func (ProxyGroup) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "proxy_groups"}}
}

func (ProxyGroup) Mixin() []ent.Mixin { return []ent.Mixin{mixins.TimeMixin{}} }

func (ProxyGroup) Fields() []ent.Field {
	return []ent.Field{field.String("name").MaxLen(100).NotEmpty().Unique()}
}

func (ProxyGroup) Edges() []ent.Edge {
	return []ent.Edge{edge.To("proxies", Proxy.Type)}
}
