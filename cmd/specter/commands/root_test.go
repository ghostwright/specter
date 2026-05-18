package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/spf13/cobra"
)

func TestRootCommandErrorOutputJSON(t *testing.T) {
	err := executeFailingRootCommand(t, "--json", "fail")

	var buf bytes.Buffer
	WriteError(&buf, err)

	var got map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("error output is not valid JSON: %v", err)
	}
	if got["error"] != "boom" {
		t.Fatalf("error = %q, want %q", got["error"], "boom")
	}
}

func TestRootCommandErrorOutputPlainText(t *testing.T) {
	err := executeFailingRootCommand(t, "fail")

	var buf bytes.Buffer
	WriteError(&buf, err)

	if got, want := buf.String(), "boom\n"; got != want {
		t.Fatalf("error output = %q, want %q", got, want)
	}
}

func executeFailingRootCommand(t *testing.T, args ...string) error {
	t.Helper()

	oldJSONOutput := jsonOutput
	failCmd := &cobra.Command{
		Use: "fail",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("boom")
		},
	}

	rootCmd.AddCommand(failCmd)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.RemoveCommand(failCmd)
		rootCmd.SetArgs(nil)
		jsonOutput = oldJSONOutput
	})

	err := Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want error")
	}

	return err
}
