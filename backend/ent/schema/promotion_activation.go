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

// PromotionActivation stores one-time activation events.
type PromotionActivation struct {
	ent.Schema
}

func (PromotionActivation) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "promotion_activations"},
	}
}

func (PromotionActivation) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("user_id"),
		field.Int64("promoter_user_id"),
		field.Time("activated_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Float("threshold_amount").SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Float("trigger_usage_amount").SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Int64("commission_record_id").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (PromotionActivation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("promoter_user_id"),
	}
}
