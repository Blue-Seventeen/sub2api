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

// PromotionScript stores reusable promotion copywriting templates.
type PromotionScript struct {
	ent.Schema
}

func (PromotionScript) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "promotion_scripts"},
	}
}

func (PromotionScript) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("name").MaxLen(100).NotEmpty(),
		field.String("category").MaxLen(32).Default("default"),
		field.String("content").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Int64("use_count").Default(0),
		field.Bool("enabled").Default(true),
		field.Int64("created_by_user_id").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (PromotionScript) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("enabled", "created_at"),
		index.Fields("category"),
	}
}
