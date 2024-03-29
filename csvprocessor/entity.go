package csvprocessor

import (
	"fmt"

	"github.com/streamingfast/substreams-graph-load/schema"
	pbentity "github.com/streamingfast/substreams-sink-entity-changes/pb/sf/substreams/sink/entity/v1"
)

const FieldTypeBigint = "Bigint"
const FieldTypeString = "String_"
const FieldTypeBigdecimal = "Bigdecimal"
const FieldTypeBytes = "Bytes"
const FieldTypeInt = "Int32"
const FieldTypeFloat = "Float"
const FieldTypeBoolean = "Boolean"

type Entity struct {
	StartBlock uint64
	Fields     map[string]interface{}
}

func blockRange(start, stop uint64) string {
	if stop == 0 {
		return fmt.Sprintf("[%d,)", start)
	}

	return fmt.Sprintf("[%d,%d)", start, stop)
}

func (e *Entity) Update(newEnt *Entity) {
	e.StartBlock = newEnt.StartBlock
	for k, v := range newEnt.Fields {
		e.Fields[k] = v
	}
}

func (e *Entity) ValidateFields(desc *schema.EntityDesc) error {
	for _, field := range desc.OrderedFields() {
		if !field.Nullable && e.Fields[field.Name] == nil {
			return fmt.Errorf("field %q cannot be nil on entity %s at ID %s", field.Name, field.Type, e.Fields["id"])
		}
	}

	return nil
}

func newEntity(in *EntityChangeAtBlockNum, desc *schema.EntityDesc) (*Entity, error) {
	if in.EntityChange.Operation == pbentity.EntityChange_OPERATION_DELETE {
		return nil, nil
	}

	e := &Entity{
		StartBlock: in.BlockNum,
	}
	e.Fields = map[string]interface{}{
		"id": in.EntityChange.ID,
	}
	for _, f := range in.EntityChange.Fields {
		normalizedName := schema.NormalizeField(f.Name)
		fieldDesc, ok := desc.Fields[normalizedName]
		if !ok {
			return nil, fmt.Errorf("invalid field %q not part of entity", normalizedName)
		}

		var expectedTypedField string

		switch fieldDesc.Type {
		case schema.FieldTypeID, schema.FieldTypeString:
			expectedTypedField = FieldTypeString
		case schema.FieldTypeBigInt:
			expectedTypedField = FieldTypeBigint
		case schema.FieldTypeBigDecimal:
			expectedTypedField = FieldTypeBigdecimal
		case schema.FieldTypeBytes:
			expectedTypedField = FieldTypeBytes
		case schema.FieldTypeInt:
			expectedTypedField = FieldTypeInt
		case schema.FieldTypeFloat:
			expectedTypedField = FieldTypeFloat
		case schema.FieldTypeBoolean:
			expectedTypedField = FieldTypeBoolean
		default:
			return nil, fmt.Errorf("invalid field type: %q", fieldDesc.Type)
		}

		if fieldDesc.Array {
			arr, ok := f.NewValue.Typed["Array"]
			if !ok {
				return nil, fmt.Errorf("invalid field %q: expected array of %s, found %+v", normalizedName, fieldDesc.Type, f.NewValue.Typed)

			}
			asMap, ok := arr.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("invalid field %q: expected array of %s, found %+v", normalizedName, fieldDesc.Type, arr)
			}
			val, ok := asMap["value"]
			if !ok {
				e.Fields[normalizedName] = []interface{}{}
				continue
			}

			array, ok := val.([]interface{})
			if !ok {
				return nil, fmt.Errorf("invalid field %q: expected array for map value, found %+v", normalizedName, val)
			}
			out := make([]interface{}, len(array))
			for i := range array {
				out[i] = array[i].(map[string]interface{})["Typed"].(map[string]interface{})[expectedTypedField]
			}
			e.Fields[normalizedName] = out

			continue
		}

		v, ok := f.NewValue.Typed[expectedTypedField]
		if !ok {
			return nil, fmt.Errorf("invalid field %q: wrong type %q, got %+v", normalizedName, fieldDesc.Type, f.NewValue.Typed)
		}
		e.Fields[normalizedName] = v
	}

	return e, nil
}

type EntityChangeAtBlockNum struct {
	EntityChange struct {
		Entity    string                          `json:"entity"`
		ID        string                          `json:"id"`
		Operation pbentity.EntityChange_Operation `json:"operation"`
		Fields    []struct {
			Name     string `json:"name"`
			NewValue struct {
				Typed map[string]interface{} `json:"Typed"`
			} `json:"new_value"`
		}
	} `json:"entity_change"`
	BlockNum uint64 `json:"block_num"`
}
