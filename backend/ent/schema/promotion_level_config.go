package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PromotionLevelConfig stores referral level configuration.
type PromotionLevelConfig struct {
	ent.Schema
}

func (PromotionLevelConfig) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "promotion_level_configs"},
	}
}

func (PromotionLevelConfig) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int("level_no").Unique(),
		field.String("level_name").MaxLen(50).NotEmpty(),
		field.Int("required_activated_invites").Default(0),
		field.Float("direct_rate").Default(0).SchemaType(map[string]string{dialect.Postgres: "decimal(8,4)"}),
		field.Float("indirect_rate").Default(0).SchemaType(map[string]string{dialect.Postgres: "decimal(8,4)"}),
		field.Int("sort_order").Default(0),
		field.Bool("enabled").Default(true),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (PromotionLevelConfig) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("level_no"),
		index.Fields("sort_order"),
	}
}
