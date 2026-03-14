package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var tagCmd = &cobra.Command{
	Use:   "tag [TAG]...",
	Short: "Extract tags from SAM file",
	Long: `Extract specified tags from SAM records from stdin.

Example:
  cat test.sam | hey tag NM MD AS
`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		processTag(args)
	},
}

func init() {
	getCmd.AddCommand(tagCmd)
	rootCmd.AddCommand(tagCmd)
}

func processTag(tags []string) {
	scanner := bufio.NewScanner(os.Stdin)
	// SAM files can have very long lines, so increase buffer size to 10MB.
	const maxCapacity = 10 * 1024 * 1024
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "@") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 11 {
			continue
		}

		results := make([]string, len(tags))
		for i, requestedTag := range tags {
			value := ""
			for _, field := range fields[11:] {
				// SAM tag format is TAG:TYPE:VALUE
				if strings.HasPrefix(field, requestedTag+":") {
					parts := strings.SplitN(field, ":", 3)
					if len(parts) == 3 {
						value = parts[2]
						break
					}
				}
			}
			results[i] = value
		}
		fmt.Println(strings.Join(results, "\t"))
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "Error reading SAM records:", err)
	}
}
