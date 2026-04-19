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

// PromotionCommissionRecord stores pending/settled/cancelled commission rows.
type PromotionCommissionRecord struct {
	ent.Schema
}

func (PromotionCommissionRecord) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "promotion_commission_records"},
	}
}

func (PromotionCommissionRecord) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("beneficiary_user_id"),
		field.Int64("source_user_id").Optional().Nillable(),
		field.Time("business_date").SchemaType(map[string]string{dialect.Postgres: "date"}),
		field.String("commission_type").MaxLen(20).NotEmpty(),
		field.Int8("relation_depth").Default(0),
		field.Int64("level_id").Optional().Nillable(),
		field.String("level_snapshot").Optional().Nillable().MaxLen(50),
		field.Float("rate_snapshot").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "decimal(8,4)"}),
		field.Float("base_amount").Default(0).SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Float("amount").SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.String("status").MaxLen(20).Default("pending"),
		field.Int64("settlement_batch_id").Optional().Nillable(),
		field.String("note").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Int64("created_by_user_id").Optional().Nillable(),
		field.Time("settled_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("cancelled_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (PromotionCommissionRecord) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("beneficiary_user_id", "status", "business_date"),
		index.Fields("status", "business_date"),
		index.Fields("source_user_id", "business_date"),
	}
}
