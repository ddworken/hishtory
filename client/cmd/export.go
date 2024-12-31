package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/ddworken/hishtory/client/data"
	"github.com/ddworken/hishtory/client/hctx"
	"github.com/ddworken/hishtory/client/lib"

	"github.com/spf13/cobra"
)

var exportJsonCmd = &cobra.Command{
	Use:   "export-json",
	Short: "Export history entries formatted in JSON lines format (as accepted by hishtory import-json, and easily parsable by other tools)",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := hctx.MakeContext()
		err := exportToJson(ctx, os.Stdout)
		lib.CheckFatalError(err)
	},
}

func structToMap(entry data.HistoryEntry) (map[string]interface{}, error) {
	inrec, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	err = json.Unmarshal(inrec, &m)
	return m, err
}

func exportToJson(ctx context.Context, w io.Writer) error {
	db := hctx.GetDb(ctx)
	chunkSize := 1000
	offset := 0
	for {
		entries, err := lib.SearchWithOffset(ctx, db, "", chunkSize, offset)
		if err != nil {
			return fmt.Errorf("failed to search for history entries with offset=%d: %w", offset, err)
		}
		if len(entries) == 0 {
			break
		}
		for _, entry := range entries {
			m, err := structToMap(*entry)
			if err != nil {
				return err
			}
			delete(m, "device_id")
			delete(m, "entry_id")
			j, err := json.Marshal(m)
			if err != nil {
				return err
			}
			_, err = w.Write(j)
			if err != nil {
				return err
			}
			_, err = w.Write([]byte("\n"))
			if err != nil {
				return err
			}
		}
		offset += chunkSize
	}
	return nil
}

func init() {
	rootCmd.AddCommand(exportJsonCmd)
}
