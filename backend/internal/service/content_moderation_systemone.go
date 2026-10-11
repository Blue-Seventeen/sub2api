package service

import "github.com/tidwall/gjson"

// System One sends object keys to the model as well as values. Ignore only
// validated protocol fields, not client-controlled extensions or question IDs.
func (collector moderationTextCollector) collectSystemOneInput(body []byte, parts *[]string) {
	root := gjson.ParseBytes(body)
	questions := root.Get("questions")
	if !questions.IsObject() {
		collector.collectSystemOneText(questions, parts)
	}
	questions.ForEach(func(id, question gjson.Result) bool {
		collector.addModerationText(parts, id.String())
		if !question.IsObject() {
			collector.collectSystemOneText(question, parts)
			return true
		}
		question.ForEach(func(field, value gjson.Result) bool {
			switch field.String() {
			case "type":
				return true
			case "instructions", "criteria":
			default:
				collector.addModerationText(parts, field.String())
			}
			collector.collectSystemOneText(value, parts)
			return true
		})
		return true
	})
	root.ForEach(func(field, value gjson.Result) bool {
		switch field.String() {
		case "model", "stream", "state", "questions":
			return true
		}
		collector.addModerationText(parts, field.String())
		collector.collectSystemOneText(value, parts)
		return true
	})
	collector.collectSystemOneText(root.Get("state"), parts)
}

func (collector moderationTextCollector) collectSystemOneText(value gjson.Result, parts *[]string) {
	switch {
	case !value.Exists():
		return
	case value.Type == gjson.String:
		collector.addModerationText(parts, value.String())
	case value.IsArray():
		value.ForEach(func(_, child gjson.Result) bool {
			collector.collectSystemOneText(child, parts)
			return true
		})
	case value.IsObject():
		value.ForEach(func(key, child gjson.Result) bool {
			collector.addModerationText(parts, key.String())
			collector.collectSystemOneText(child, parts)
			return true
		})
	}
}
