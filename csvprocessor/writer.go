package csvprocessor

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	"github.com/streamingfast/bstream"
	"github.com/streamingfast/dstore"
	"github.com/streamingfast/substreams-sink-graphcsv/schema"
)

type WriterManager struct {
	current      *Writer
	currentRange *bstream.Range
	bundleSize   uint64
	store        dstore.Store
}

func NewWriterManager(bundleSize uint64, store dstore.Store) *WriterManager {
	return &WriterManager{
		bundleSize: bundleSize,
		store:      store,
	}
}

func (wm *WriterManager) setNewWriter(ctx context.Context, blockNum uint64) error {
	var nextRange *bstream.Range

	if wm.currentRange == nil {
		r, err := bstream.NewRangeContaining(blockNum, wm.bundleSize)
		if err != nil {
			return err
		}
		nextRange = r
	} else {
		r := wm.currentRange
		for {
			if r.Contains(blockNum) {
				nextRange = r
				break
			}
			r = r.Next(wm.bundleSize)
		}
	}

	writer, err := NewWriter(ctx, wm.store, fileNameFromRange(nextRange))
	if err != nil {
		return err
	}

	wm.current = writer
	wm.currentRange = nextRange
	return nil
}

func (wm *WriterManager) Roll(ctx context.Context, blockNum uint64) error {
	if wm.current == nil {
		return wm.setNewWriter(ctx, blockNum)
	}
	if wm.currentRange.ReachedEndBlock(blockNum) {
		if err := wm.current.Close(); err != nil {
			return err
		}
		return wm.setNewWriter(ctx, blockNum)
	}
	return nil
}

func (wm *WriterManager) Close() error {
	return wm.current.Close()
}

func (wm *WriterManager) Write(e *Entity, desc *schema.EntityDesc, stopBlock uint64) error {
	return wm.current.Write(e, desc, stopBlock)
}

type Writer struct {
	writer    *io.PipeWriter
	done      chan struct{}
	csvWriter *csv.Writer
	filename  string
}

func NewWriter(ctx context.Context, store dstore.Store, filename string) (*Writer, error) {
	reader, writer := io.Pipe()
	csvWriter := csv.NewWriter(writer)

	ce := &Writer{
		filename:  filename,
		csvWriter: csvWriter,
		writer:    writer,
		done:      make(chan struct{}),
	}

	go func() {
		err := store.WriteObject(ctx, filename, reader)
		if err != nil {
			// todo: better handle error
			panic(fmt.Errorf("failed writting object in file object %q: %w", filename, err))
		}
		close(ce.done)
	}()

	return ce, nil
}

func (c *Writer) Write(e *Entity, desc *schema.EntityDesc, stopBlock uint64) error {
	records := []string{
		formatField(e.Fields["id"], schema.FieldTypeID, false, false),
		blockRange(e.StartBlock, stopBlock),
	}

	for _, f := range desc.OrderedFields() {
		if f.Name == "id" {
			continue
		}
		out := formatField(e.Fields[f.Name], f.Type, f.Array, f.Nullable)
		records = append(records, out)
	}

	if err := c.csvWriter.Write(records); err != nil {
		return err
	}
	return nil
}

func panicIfNotNullable(isNullable bool) {
	if !isNullable {
		panic("invalid field: not nullable")
	}
}

func toEscapedStringArray(in []interface{}, formatter string) string {
	outs := make([]string, len(in))
	for i := range in {
		formatted := fmt.Sprintf(formatter, in[i])
		outs[i] = strings.ReplaceAll(strings.ReplaceAll(formatted, `\`, `\\`), `,`, `\,`)
	}
	return "{" + strings.Join(outs, ",") + "}"
}

func formatField(f interface{}, t schema.FieldType, isArray, isNullable bool) string {
	switch t {
	case schema.FieldTypeID, schema.FieldTypeString:
		if f == nil {
			if isNullable {
				return "NULL"
			}
			return ""
		}
		if isArray {
			return toEscapedStringArray(f.([]interface{}), "%s")
		}
		return fmt.Sprintf("%s", f)
	case schema.FieldTypeBytes:
		if f == nil {
			if isNullable {
				return "NULL"
			}
			return ""
		}
		if isArray {
			return toEscapedStringArray(f.([]interface{}), "%s")
		}
		return fmt.Sprintf("%s", f)
	case schema.FieldTypeBigInt:
		if f == nil {
			if isNullable {
				return "NULL"
			}
			return "0"
		}
		if isArray {
			return toEscapedStringArray(f.([]interface{}), "%s")
		}
		return fmt.Sprintf("%s", f)
	case schema.FieldTypeBigDecimal:
		if f == nil {
			if isNullable {
				return "NULL"
			}
			return "0"
		}
		if isArray {
			return toEscapedStringArray(f.([]interface{}), "%s")
		}
		return fmt.Sprintf("%s", f)
	case schema.FieldTypeInt:
		if f == nil {
			if isNullable {
				return "NULL"
			}
			return "0"
		}
		if isArray {
			return toEscapedStringArray(f.([]interface{}), "%d")
		}
		return fmt.Sprintf("%d", f)
	case schema.FieldTypeFloat:
		if f == nil {
			if isNullable {
				return "NULL"
			}
			return "0"
		}
		if isArray {
			return toEscapedStringArray(f.([]interface{}), "%f")
		}
		return fmt.Sprintf("%f", f)
	case schema.FieldTypeBoolean:
		if f == nil {
			if isNullable {
				return "NULL"
			}
			return "false"
		}
		if isArray {
			return toEscapedStringArray(f.([]interface{}), "%t")
		}
		return fmt.Sprintf("%t", f)
	default:
		panic(fmt.Errorf("invalid field type: %q", t))
	}
}

func (c *Writer) Close() error {
	c.csvWriter.Flush()
	if err := c.csvWriter.Error(); err != nil {
		return fmt.Errorf("error flushing csv encoder: %w", err)
	}

	if err := c.writer.Close(); err != nil {
		return fmt.Errorf("closing csv writer: %w", err)
	}
	<-c.done
	return nil
}

func fileNameFromRange(r *bstream.Range) string {
	return fmt.Sprintf("%d-%d", r.StartBlock(), *r.EndBlock()-1) // endBlock should always be set in those ranges
}