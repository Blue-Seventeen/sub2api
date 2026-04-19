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

// PromotionSettlementBatch stores daily settlement execution batches.
type PromotionSettlementBatch struct {
	ent.Schema
}

func (PromotionSettlementBatch) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "promotion_settlement_batches"},
	}
}

func (PromotionSettlementBatch) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Time("business_date").SchemaType(map[string]string{dialect.Postgres: "date"}).Unique(),
		field.String("status").MaxLen(20).Default("running"),
		field.Int("total_records").Default(0),
		field.Float("total_amount").Default(0).SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Int64("executed_by_user_id").Optional().Nillable(),
		field.Time("executed_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("note").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (PromotionSettlementBatch) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("business_date"),
		index.Fields("status"),
	}
}
