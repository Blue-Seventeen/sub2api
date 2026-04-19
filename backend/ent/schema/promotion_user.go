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

// PromotionUser stores referral code and parent binding for a user.
type PromotionUser struct {
	ent.Schema
}

func (PromotionUser) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "promotion_users"},
	}
}

func (PromotionUser) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").
			StorageKey("user_id"),
		field.String("invite_code").
			MaxLen(32).
			NotEmpty().
			Unique(),
		field.Int64("parent_user_id").
			Optional().
			Nillable(),
		field.String("binding_source").
			MaxLen(20).
			Default("self"),
		field.String("bound_note").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("bound_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").
			Default(time.Now).
			Immutable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (PromotionUser) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("invite_code"),
		index.Fields("parent_user_id"),
	}
}
