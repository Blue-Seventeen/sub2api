package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Proxy holds the schema definition for the Proxy entity.
type Proxy struct {
	ent.Schema
}

func (Proxy) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "proxies"},
	}
}

func (Proxy) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
		mixins.SoftDeleteMixin{},
	}
}

func (Proxy) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			MaxLen(100).
			NotEmpty(),
		field.String("protocol").
			MaxLen(20).
			NotEmpty(),
		field.String("host").
			MaxLen(255).
			NotEmpty(),
		field.Int("port"),
		field.String("username").
			MaxLen(100).
			Optional().
			Nillable(),
		field.String("password").
			MaxLen(100).
			Optional().
			Nillable(),
		field.String("status").
			MaxLen(20).
			Default("active"),
		field.String("source_type").
			MaxLen(40).
			Default("manual"),
		field.Int64("subscription_id").
			Optional().
			Nillable(),
		field.Time("expires_at").
			Optional().
			Nillable().
			Comment("Proxy expiration time (NULL means never expires)."),
		field.String("fallback_mode").
			MaxLen(20).
			Default("none").
			Comment("Fallback target on expiry: none | proxy | direct."),
		field.Int64("backup_proxy_id").
			Optional().
			Nillable().
			Comment("Backup proxy id when fallback_mode=proxy (self-reference)."),
		field.Int("expiry_warn_days").
			Default(7).
			Comment("Days before expiry to flag as expiring-soon (per proxy)."),
	}
}

// Edges defines proxy relationships.
func (Proxy) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("accounts", Account.Type).
			Ref("proxy"),
		edge.To("backup_proxy", Proxy.Type).
			Field("backup_proxy_id").
			Unique(),
	}
}

func (Proxy) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status"),
		index.Fields("deleted_at"),
		index.Fields("source_type"),
		index.Fields("subscription_id"),
		index.Fields("expires_at"),
		index.Fields("backup_proxy_id"),
	}
}
