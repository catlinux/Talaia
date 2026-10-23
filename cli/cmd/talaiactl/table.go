package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// writeTable prints an aligned plain-text table.
func writeTable(out io.Writer, headers []string, rows [][]string) error {
	writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, strings.Join(headers, "\t")); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(writer, strings.Join(row, "\t")); err != nil {
			return err
		}
	}
	return writer.Flush()
}

// writeRawJSON prints the response body as indented JSON. It is used only for
// successful responses, which are JSON by contract.
func writeRawJSON(out io.Writer, raw []byte) error {
	var indented bytes.Buffer
	if err := json.Indent(&indented, raw, "", "  "); err != nil {
		if _, writeErr := out.Write(raw); writeErr != nil {
			return writeErr
		}
		if len(raw) == 0 || raw[len(raw)-1] != '\n' {
			_, err = fmt.Fprintln(out)
		}
		return err
	}
	if _, err := fmt.Fprintln(out, indented.String()); err != nil {
		return err
	}
	return nil
}
